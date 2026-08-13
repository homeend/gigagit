package engine

import (
	"context"
	"fmt"
	"strings"
)

// CherryPick applies Commits (oldest first) onto the current branch in this
// worktree, one new commit each, as a single sequencer run. If the tree is
// dirty it autostashes first and restores afterward (like SmartMerge). A
// conflicted pick forks via the "cherry-pick-conflict" decision:
// keep-conflicts leaves the tree — and, mid-sequence, the paused sequencer —
// for manual resolution (the op returns an error; `cherry-pick --continue`
// finishes the rest), abort runs `git cherry-pick --abort`, which rewinds the
// WHOLE sequence (all-or-nothing). On a kept conflict any autostash is
// preserved rather than popped onto the conflicted tree.
//
// A commit whose change is already on the branch stops the sequencer with a
// clean tree: a single-commit op auto-aborts with a legible error (never
// trap), a multi-commit op skips it and continues.
type CherryPick struct {
	Commits []string
}

var _ Operation = CherryPick{}

func (op CherryPick) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if len(op.Commits) == 0 {
		return Result{}, fmt.Errorf("cherry-pick: Commit is required")
	}

	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := deps.Repo.StashPush(ctx, "gg-autostash:cherry-pick", nil, false); err != nil {
			return Result{}, err
		}
		stashed = true
	}

	deps.emit(ctx, Progress{Step: "cherry-picking", Detail: strings.Join(op.Commits, " ")})
	pickErr := deps.Repo.CherryPick(ctx, "", op.Commits...)
	skipped := 0
	for pickErr != nil {
		inPick, stateErr := deps.Repo.CherryPickInProgress(ctx, "")
		if stateErr != nil {
			return Result{}, fmt.Errorf("cherry-pick %s: %v (state check: %w)", op.describe(), pickErr, stateErr)
		}
		if !inPick {
			// Refused outright (bad ref, an empty/redundant commit git rejects
			// before starting): nothing to resolve. The autostash is preserved.
			return Result{}, fmt.Errorf("cherry-pick %s: %w", op.describe(), pickErr)
		}
		st, sErr := deps.Repo.Status(ctx)
		if sErr != nil || st.Counts().Conflicted > 0 {
			return op.conflict(ctx, deps, stashed)
		}
		// In progress but with NO conflicted files = an empty/already-applied
		// commit (git stops with CHERRY_PICK_HEAD set yet a clean tree).
		if len(op.Commits) == 1 {
			// There is nothing to resolve and `--continue` would error, so don't
			// offer the conflict fork: auto-abort to a clean state and return a
			// legible error. Never trap the user.
			_ = deps.Repo.CherryPickAbort(ctx, "")
			return Result{}, fmt.Errorf("cherry-pick %s: nothing to apply (the commit is already on this branch); aborted", op.Commits[0])
		}
		if skipped >= len(op.Commits) {
			// --skip is not advancing the sequencer; bail instead of spinning.
			return Result{}, fmt.Errorf("cherry-pick %s: skip made no progress: %w", op.describe(), pickErr)
		}
		deps.emit(ctx, Progress{Step: "skipping already applied"})
		skipped++
		pickErr = deps.Repo.CherryPickSkip(ctx, "")
	}

	applied := len(op.Commits) - skipped
	var res Result
	switch {
	case len(op.Commits) == 1:
		res = Result{Changed: true}.WithSummary("cherry-picked %s", op.Commits[0])
	case skipped == 0:
		res = Result{Changed: true}.WithSummary("cherry-picked %d commits", applied)
	case applied == 0:
		res = Result{Changed: false}.WithSummary("nothing to apply (all %d commits already on this branch)", len(op.Commits))
	default:
		res = Result{Changed: true}.WithSummary("cherry-picked %d of %d commits (the rest were already on this branch)", applied, len(op.Commits))
	}
	return op.restore(ctx, deps, stashed, res)
}

// conflict runs the "cherry-pick-conflict" fork for a stopped pick with
// unmerged paths. The prompt names the commit the sequencer is stopped on.
func (op CherryPick) conflict(ctx context.Context, deps OpDeps, stashed bool) (Result, error) {
	name := op.Commits[0]
	if len(op.Commits) > 1 {
		if s, err := deps.Repo.CherryPickHeadSummary(ctx, ""); err == nil && s != "" {
			name = s
		}
	}
	choice, derr := deps.decide(ctx, PromptReq("cherry-pick-conflict", "Cherry-picking %s hit conflicts", []string{"keep-conflicts", "abort"}, name))
	if derr != nil {
		return Result{}, derr
	}
	if choice.Option == "keep-conflicts" {
		res := Result{Changed: true}.WithSummary("cherry-pick of %s has conflicts (left in tree)", name)
		if stashed {
			res = res.AppendSummary(" (your changes remain stashed)")
		}
		return res, fmt.Errorf("cherry-pick conflict: %s", name)
	}
	if err := deps.Repo.CherryPickAbort(ctx, ""); err != nil {
		return Result{}, fmt.Errorf("cherry-pick: abort failed: %w", err)
	}
	// Tree is back to the pre-pick tip (a sequence rewinds wholesale); safe to
	// restore the autostash.
	res := Result{Changed: false}.WithSummary("aborted: cherry-pick %s", op.Commits[0])
	if len(op.Commits) > 1 {
		res = Result{Changed: false}.WithSummary("aborted: cherry-pick of %d commits (nothing applied)", len(op.Commits))
	}
	return op.restore(ctx, deps, stashed, res)
}

// describe names the op's target in error prose: the lone hash, or the count.
func (op CherryPick) describe() string {
	if len(op.Commits) == 1 {
		return op.Commits[0]
	}
	return fmt.Sprintf("of %d commits", len(op.Commits))
}

// restore pops the autostash (if any) onto the now-clean tree. A pop conflict
// surfaces like SmartMerge's: the changes are preserved in the stash.
func (op CherryPick) restore(ctx context.Context, deps OpDeps, stashed bool, res Result) (Result, error) {
	if !stashed {
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "restoring changes"})
	if err := deps.Repo.StashPop(ctx, ""); err != nil {
		return res.AppendSummary("; restore conflicted (changes preserved in stash)"),
			fmt.Errorf("stash pop conflict after cherry-pick %s: %w", op.describe(), err)
	}
	return res, nil
}
