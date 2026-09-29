package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// RecycleDirtyDecisionID is raised when the target worktree has staged,
// unstaged or untracked changes: commit them on the branch that is leaving,
// shelve them (one shelf file set, then discard), discard them (untracked
// files are deleted, ignored files kept), or abort.
const RecycleDirtyDecisionID = "recycle.dirty"

// RecycleCommitLayout is the timestamp layout in the automated commit
// subject (local time).
const RecycleCommitLayout = "2006-01-02 15:04"

// RecycleCommitMessage is the subject of the commit "commit" makes.
func RecycleCommitMessage(now time.Time) string {
	return "Committed changes due to worktree recycle " + now.Format(RecycleCommitLayout)
}

// RecycleWorktree checks Branch out in the existing worktree at Dir — a
// worktree gg is NOT running in — replacing whatever it has checked out.
// Uncommitted work there is handled through the recycle.dirty decision
// before the switch; every pre-check runs before any mutation. Default
// TreeWrite reservation: the gate is keyed by the git common dir, so it
// already covers the other worktree.
type RecycleWorktree struct {
	Dir    string // target worktree top level
	Branch string // local branch to check out there
	// RemoteRef, when set ("origin/foo"), is checked out first as Branch —
	// SmartCheckout{Intent: Stay} inline, after the target's own refusals and
	// before the dirty prompt: a missing Branch is created tracking it, an
	// existing one fast-forwarded; a diverged one refuses (CheckoutDivergedError)
	// with the target untouched. An abort at the prompt keeps the branch.
	RemoteRef string
	Now       func() time.Time // clock for the commit message; nil = time.Now
}

var _ Operation = RecycleWorktree{}

func (op RecycleWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Dir == "" || op.Branch == "" {
		return Result{}, fmt.Errorf("recycle worktree: Dir and Branch are required")
	}
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return Result{}, err
	}
	var entry *model.Worktree
	for i := range wts {
		w := &wts[i]
		if w.Bare {
			continue
		}
		if samePath(w.Path, op.Dir) {
			entry = w
		}
		if w.Branch == op.Branch {
			return Result{}, fmt.Errorf("%s is already checked out in %s", op.Branch, w.Path)
		}
	}
	if entry == nil {
		return Result{}, fmt.Errorf("%s is not a worktree of this repository", filepath.Clean(op.Dir))
	}
	target := entry.Path
	// The worktree gg runs in is "Switch to branch" territory; a CLI caller
	// can still name it, so refuse here (style of RemoveWorktree).
	if top, err := deps.Repo.TopLevel(ctx); err == nil && samePath(target, top) {
		return Result{}, fmt.Errorf("cannot recycle the worktree you are in (%s)", target)
	}

	wt, err := deps.repoAt(target)
	if err != nil {
		return Result{}, err
	}
	gitDir, err := wt.GitDir(ctx)
	if err != nil {
		return Result{}, err
	}
	if paused := git.PausedOpIn(gitDir); paused != "" {
		return Result{}, fmt.Errorf("%s has a %s in progress", target, paused)
	}
	// Per-worktree locks (index.lock, HEAD.lock) are what block a switch
	// there; common-dir locks belong to whoever holds the reservation now.
	if locks := git.LockFiles(gitDir); len(locks) > 0 {
		return Result{}, fmt.Errorf("%s is locked (%s)", target, locks[0].Name)
	}

	if op.RemoteRef != "" {
		if _, err := (SmartCheckout{RemoteRef: op.RemoteRef, Local: op.Branch, Intent: CheckoutStay}).Run(ctx, deps); err != nil {
			return Result{}, err
		}
	}

	// The leaving branch comes from the worktree list (no extra git call;
	// "" = detached HEAD).
	old := entry.Branch
	if old == "" {
		old = "detached"
	}
	st, err := wt.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	c := st.Counts()
	committed, shelved, discarded := "", "", false
	if c.Staged+c.Unstaged+c.Conflicted+c.Untracked > 0 {
		resp, err := deps.decide(ctx, PromptReq(RecycleDirtyDecisionID,
			"%s has uncommitted changes on %s", []string{"commit", "shelve", "discard", "abort"}, target, old))
		if err != nil {
			return Result{}, err
		}
		switch resp.Option {
		case "commit":
			deps.emit(ctx, Progress{Step: "committing", Detail: target})
			if err := wt.StageAll(ctx); err != nil {
				return Result{}, err
			}
			now := op.Now
			if now == nil {
				now = time.Now
			}
			if err := wt.Commit(ctx, RecycleCommitMessage(now()), false, false); err != nil {
				return Result{}, err
			}
			sha := ""
			if line, lerr := wt.CommitLine(ctx, "HEAD"); lerr == nil {
				sha = line.Hash
			}
			committed = sha
		case "shelve":
			deps.emit(ctx, Progress{Step: "shelving", Detail: target})
			// Everything into the index first (untracked files too): the seam
			// shelves the INDEX, the one place staged, unstaged and new work
			// sit together. Nothing is discarded unless the set is stored.
			if err := wt.StageAll(ctx); err != nil {
				return Result{}, err
			}
			e, err := deps.shelveStaged(ctx, target, old)
			if err != nil {
				return Result{}, err
			}
			if err := discardAll(ctx, wt); err != nil {
				return Result{}, err
			}
			shelved = e.Label
		case "discard":
			deps.emit(ctx, Progress{Step: "discarding", Detail: target})
			if err := discardAll(ctx, wt); err != nil {
				return Result{}, err
			}
			discarded = true
		default:
			if op.RemoteRef != "" {
				return Result{Changed: true}.WithSummary("recycle cancelled; checked out %s as %s", op.RemoteRef, op.Branch), nil
			}
			return Result{}.WithSummary("recycle cancelled"), nil
		}
	}

	deps.emit(ctx, Progress{Step: "switching", Detail: op.Branch})
	if err := wt.Switch(ctx, op.Branch); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("recycled %s → %s in %s", old, op.Branch, target)
	if op.RemoteRef != "" {
		res = res.AppendSummary(" from %s", op.RemoteRef)
	}
	switch {
	case committed != "":
		res = res.AppendSummary("; committed %s", committed)
	case shelved != "":
		res = res.AppendSummary("; shelved as %q", shelved)
	case discarded:
		res = res.AppendSummary("; changes discarded")
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// discardAll throws away the worktree's staged, unstaged and untracked work.
// NOT Discard{All}: its `restore --worktree` deliberately keeps staged hunks,
// which `git switch` would then carry onto the target branch. A hard reset
// drops index AND tree; clean on the repo-root pathspec (without -x) then
// removes untracked files and keeps ignored ones. Both run even if the first
// fails.
func discardAll(ctx context.Context, wt GitOps) error {
	var errs []error
	if err := wt.Reset(ctx, "hard", "HEAD"); err != nil {
		errs = append(errs, fmt.Errorf("reset: %w", err))
	}
	if err := wt.CleanUntracked(ctx, []string{":/"}); err != nil {
		errs = append(errs, fmt.Errorf("clean: %w", err))
	}
	return errors.Join(errs...)
}
