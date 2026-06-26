package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/pusherr"
)

// Commit stages (optionally) and commits with a message. Amend rewrites the
// last commit instead of creating a new one.
type Commit struct {
	Message string
	All     bool
	Amend   bool
}

func (op Commit) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "committing", Detail: op.Message})
	if err := deps.Repo.Commit(ctx, op.Message, op.All, op.Amend); err != nil {
		return Result{}, err
	}
	summary := "committed"
	if op.Amend {
		summary = "amended"
	}
	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// Push pushes branch to remote, optionally setting upstream. When Force is set,
// the op asks the "push-force" decision (force-with-lease / force / abort) — the
// modal both lets the user pick a lease-protected or plain force and confirms a
// history-overwriting push; esc lands on abort.
//
// A non-Force push that the remote rejects for being behind (non-fast-forward)
// is not a dead-end: the op raises the "push-rejected" decision (rebase / force
// / abort) and acts on it — rebase replays the local commits onto the remote
// tip and re-pushes, force chains the push-force confirm, abort is a no-op. esc
// lands on abort. Any other failure (credentials, hook, network) is returned
// unchanged.
type Push struct {
	Remote      string
	Branch      string
	SetUpstream bool
	Force       bool
}

func (op Push) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Force {
		force, ok, err := op.decideForce(ctx, deps)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{Summary: "push cancelled", Changed: false}, nil
		}
		return op.push(ctx, deps, force)
	}

	deps.emit(ctx, Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, git.PushNoForce)
	if err == nil {
		res := Result{Summary: "pushed", Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	if !pusherr.IsNonFastForward(err.Error()) {
		return Result{}, err // credentials / hook / network: not recoverable here
	}
	return op.recoverRejected(ctx, deps)
}

// decideForce asks the push-force decision and maps it to a force mode. ok=false
// means the user aborted. The safer lease-protected option leads (index 0) so a
// modal's default enter never triggers a plain overwrite.
func (op Push) decideForce(ctx context.Context, deps OpDeps) (git.PushForce, bool, error) {
	choice, err := deps.decide(ctx, DecisionRequest{
		ID:      "push-force",
		Prompt:  "Force-push " + op.Branch + " to " + op.Remote + " (overwrites the remote branch)",
		Options: []string{"force-with-lease", "force", "abort"},
	})
	if err != nil {
		return git.PushNoForce, false, err
	}
	switch choice.Option {
	case "force-with-lease":
		return git.PushForceWithLease, true, nil
	case "force":
		return git.PushForcePlain, true, nil
	case "abort", "":
		return git.PushNoForce, false, nil
	default:
		return git.PushNoForce, false, fmt.Errorf("push: unknown force mode %q", choice.Option)
	}
}

// push performs the push with the given force mode and emits Done on success.
func (op Push) push(ctx context.Context, deps OpDeps, force git.PushForce) (Result, error) {
	deps.emit(ctx, Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, force); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pushed", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// recoverRejected handles a non-fast-forward rejection on a plain push: rebase
// onto the remote then re-push, chain the force decision, or abort. esc lands on
// abort.
func (op Push) recoverRejected(ctx context.Context, deps OpDeps) (Result, error) {
	// The rebase recovery rebases the CURRENT HEAD onto the remote tip, so it is
	// valid only when the rejected push targets the checked-out branch. The
	// Branches-panel "Push <branch>" action can push a non-current branch; for
	// that, offer force/abort only — a rebase would rewrite the wrong branch. If
	// HEAD can't be determined (detached, error), treat it as non-current.
	allowRebase := false
	if cur, cerr := deps.Repo.CurrentBranch(ctx); cerr == nil && cur == op.Branch {
		allowRebase = true
	}
	opts := []string{"force", "abort"}
	prompt := "Remote has new commits on " + op.Branch + " — force-push or abort"
	if allowRebase {
		opts = []string{"rebase", "force", "abort"}
		prompt = "Remote has new commits on " + op.Branch + " — rebase onto them, force-push, or abort"
	}
	choice, err := deps.decide(ctx, DecisionRequest{
		ID:      "push-rejected",
		Prompt:  prompt,
		Options: opts,
	})
	if err != nil {
		return Result{}, err
	}
	switch choice.Option {
	case "rebase":
		if !allowRebase {
			return Result{}, fmt.Errorf("push: cannot rebase %q — it is not the current branch", op.Branch)
		}
		return op.rebaseThenPush(ctx, deps)
	case "force":
		force, ok, derr := op.decideForce(ctx, deps)
		if derr != nil {
			return Result{}, derr
		}
		if !ok {
			return Result{Summary: "push cancelled", Changed: false}, nil
		}
		return op.push(ctx, deps, force)
	case "abort", "":
		return Result{Summary: "push cancelled", Changed: false}, nil
	default:
		return Result{}, fmt.Errorf("push: unknown recovery %q", choice.Option)
	}
}

// rebaseThenPush replays the local commits on top of the remote tip (pull
// --rebase), then re-pushes once. A rebase conflict forks via the existing
// rebase-conflict decision: keep-conflicts leaves the tree for `git rebase
// --continue` (the TUI conflict process picks it up) and returns an error;
// abort restores the pre-rebase tip. After a clean rebase the branch is ahead,
// so the re-push fast-forwards. The recovery runs once — a second rejection is
// surfaced, never re-entered, so the op cannot loop.
func (op Push) rebaseThenPush(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "pull --rebase", Detail: op.Remote + " " + op.Branch})
	if rebaseErr := deps.Repo.Pull(ctx, op.Remote, op.Branch, git.PullRebase); rebaseErr != nil {
		inRebase, stateErr := deps.Repo.RebaseInProgress(ctx, "")
		if stateErr != nil {
			return Result{}, fmt.Errorf("push rebase: %v (state check: %w)", rebaseErr, stateErr)
		}
		if !inRebase {
			return Result{}, fmt.Errorf("push rebase: %w", rebaseErr)
		}
		choice, derr := deps.decide(ctx, DecisionRequest{
			ID:      "rebase-conflict",
			Prompt:  "Rebasing " + op.Branch + " onto " + op.Remote + "/" + op.Branch + " hit conflicts",
			Options: []string{"keep-conflicts", "abort"},
		})
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option == "keep-conflicts" {
			return Result{Summary: "rebase paused on a conflict (resolve, then `git rebase --continue`, then push)", Changed: true},
				fmt.Errorf("push rebase conflict: %s", op.Branch)
		}
		if err := deps.Repo.RebaseAbort(ctx, ""); err != nil {
			return Result{}, fmt.Errorf("push rebase: abort failed: %w", err)
		}
		return Result{Summary: "push cancelled (rebase aborted)", Changed: false}, nil
	}

	deps.emit(ctx, Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, git.PushNoForce); err != nil {
		return Result{}, err // second rejection or other error: surface, no loop
	}
	res := Result{Summary: "rebased and pushed", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// Stash saves the working-tree changes for Paths (all when empty) to a new stash.
type Stash struct {
	Message          string
	Paths            []string
	IncludeUntracked bool
}

func (op Stash) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "stashing", Detail: op.Message})
	if err := deps.Repo.StashPush(ctx, op.Message, op.Paths, op.IncludeUntracked); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "stashed: " + op.Message, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashApply restores a stash, keeping it in the list.
type StashApply struct{ Ref string }

func (op StashApply) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "applying stash", Detail: op.Ref})
	if err := deps.Repo.StashApply(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "applied " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashPop restores a stash and drops it.
type StashPop struct{ Ref string }

func (op StashPop) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "popping stash", Detail: op.Ref})
	if err := deps.Repo.StashPop(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "popped " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// StashDrop deletes a stash without applying it.
type StashDrop struct{ Ref string }

func (op StashDrop) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "dropping stash", Detail: op.Ref})
	if err := deps.Repo.StashDrop(ctx, op.Ref); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "dropped " + op.Ref, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// Compile-time checks that these satisfy Operation.
var (
	_ Operation = Commit{}
	_ Operation = Push{}
	_ Operation = Stash{}
	_ Operation = StashApply{}
	_ Operation = StashPop{}
	_ Operation = StashDrop{}
)
