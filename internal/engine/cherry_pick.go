package engine

import (
	"context"
	"fmt"
)

// CherryPick applies Commit onto the current branch in this worktree as a new
// commit. If the tree is dirty it autostashes first and restores afterward
// (like SmartMerge). A conflicted pick forks via the "cherry-pick-conflict"
// decision: keep-conflicts leaves the tree for manual resolution (the op
// returns an error), abort runs `git cherry-pick --abort`. On a kept conflict
// any autostash is preserved rather than popped onto the conflicted tree.
type CherryPick struct {
	Commit string
}

var _ Operation = CherryPick{}

func (op CherryPick) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Commit == "" {
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

	deps.emit(ctx, Progress{Step: "cherry-picking", Detail: op.Commit})
	pickErr := deps.Repo.CherryPick(ctx, "", op.Commit)
	if pickErr == nil {
		return op.restore(ctx, deps, stashed, Result{Summary: "cherry-picked " + op.Commit, Changed: true})
	}

	inPick, stateErr := deps.Repo.CherryPickInProgress(ctx, "")
	if stateErr != nil {
		return Result{}, fmt.Errorf("cherry-pick %s: %v (state check: %w)", op.Commit, pickErr, stateErr)
	}
	if !inPick {
		// Refused outright (bad ref, an empty/redundant commit git rejects before
		// starting): nothing to resolve. The autostash is preserved.
		return Result{}, fmt.Errorf("cherry-pick %s: %w", op.Commit, pickErr)
	}

	// In progress but with NO conflicted files = an empty/already-applied commit
	// (git stops with CHERRY_PICK_HEAD set yet a clean tree). There is nothing to
	// resolve and `--continue` would error, so don't offer the conflict fork:
	// auto-abort to a clean state and return a legible error. Never trap the user.
	if st, sErr := deps.Repo.Status(ctx); sErr == nil && st.Counts().Conflicted == 0 {
		_ = deps.Repo.CherryPickAbort(ctx, "")
		return Result{}, fmt.Errorf("cherry-pick %s: nothing to apply (the commit is already on this branch); aborted", op.Commit)
	}

	choice, derr := deps.decide(ctx, DecisionRequest{
		ID:      "cherry-pick-conflict",
		Prompt:  "Cherry-picking " + op.Commit + " hit conflicts",
		Options: []string{"keep-conflicts", "abort"},
	})
	if derr != nil {
		return Result{}, derr
	}
	if choice.Option == "keep-conflicts" {
		summary := "cherry-pick of " + op.Commit + " has conflicts (left in tree)"
		if stashed {
			summary += " (your changes remain stashed)"
		}
		return Result{Summary: summary, Changed: true}, fmt.Errorf("cherry-pick conflict: %s", op.Commit)
	}
	if err := deps.Repo.CherryPickAbort(ctx, ""); err != nil {
		return Result{}, fmt.Errorf("cherry-pick: abort failed: %w", err)
	}
	// Tree is back to the pre-pick tip; safe to restore the autostash.
	return op.restore(ctx, deps, stashed, Result{Summary: "aborted: cherry-pick " + op.Commit, Changed: false})
}

// restore pops the autostash (if any) onto the now-clean tree. A pop conflict
// surfaces like SmartMerge's: the changes are preserved in the stash.
func (op CherryPick) restore(ctx context.Context, deps OpDeps, stashed bool, res Result) (Result, error) {
	if !stashed {
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "restoring changes"})
	if err := deps.Repo.StashPop(ctx, ""); err != nil {
		return Result{Summary: res.Summary + "; restore conflicted (changes preserved in stash)", Changed: res.Changed},
			fmt.Errorf("stash pop conflict after cherry-pick %s: %w", op.Commit, err)
	}
	return res, nil
}
