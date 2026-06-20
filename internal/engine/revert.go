package engine

import (
	"context"
	"fmt"
)

// Revert creates a new commit on the current branch in this worktree that undoes
// Commit. If the tree is dirty it autostashes first and restores afterward (like
// CherryPick). A conflicted revert forks via the "revert-conflict" decision:
// keep-conflicts leaves the tree for manual resolution (the op returns an error),
// abort runs `git revert --abort`. On a kept conflict any autostash is preserved
// rather than popped onto the conflicted tree. Reverting a merge commit is
// refused outright by git (it needs -m <parent>); that surfaces as a legible
// error with no dangling state.
type Revert struct {
	Commit string
}

var _ Operation = Revert{}

func (op Revert) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Commit == "" {
		return Result{}, fmt.Errorf("revert: Commit is required")
	}

	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := deps.Repo.StashPush(ctx, "gg-autostash:revert", nil, false); err != nil {
			return Result{}, err
		}
		stashed = true
	}

	deps.emit(ctx, Progress{Step: "reverting", Detail: op.Commit})
	revErr := deps.Repo.Revert(ctx, "", op.Commit)
	if revErr == nil {
		return op.restore(ctx, deps, stashed, Result{Summary: "reverted " + op.Commit, Changed: true})
	}

	inRevert, stateErr := deps.Repo.RevertInProgress(ctx, "")
	if stateErr != nil {
		return Result{}, fmt.Errorf("revert %s: %v (state check: %w)", op.Commit, revErr, stateErr)
	}
	if !inRevert {
		// Refused outright (bad ref, or a merge commit without -m): nothing to
		// resolve. The autostash is preserved. git's message is legible.
		return Result{}, fmt.Errorf("revert %s: %w", op.Commit, revErr)
	}

	// In progress but with NO conflicted files = an empty/redundant revert.
	// Defensive: empirically `git revert` of an already-undone change refuses
	// outright (REVERT_HEAD NOT set → the !inRevert branch above), unlike
	// cherry-pick which leaves CHERRY_PICK_HEAD set; this guard exists so any
	// git that DID leave REVERT_HEAD set with a clean tree still can't trap the
	// resolver (`--continue` would error). Auto-abort to a clean state.
	if st, sErr := deps.Repo.Status(ctx); sErr == nil && st.Counts().Conflicted == 0 {
		_ = deps.Repo.RevertAbort(ctx, "")
		return Result{}, fmt.Errorf("revert %s: nothing to undo (already reverted); aborted", op.Commit)
	}

	choice, derr := deps.decide(ctx, DecisionRequest{
		ID:      "revert-conflict",
		Prompt:  "Reverting " + op.Commit + " hit conflicts",
		Options: []string{"keep-conflicts", "abort"},
	})
	if derr != nil {
		return Result{}, derr
	}
	if choice.Option == "keep-conflicts" {
		summary := "revert of " + op.Commit + " has conflicts (left in tree)"
		if stashed {
			summary += " (your changes remain stashed)"
		}
		return Result{Summary: summary, Changed: true}, fmt.Errorf("revert conflict: %s", op.Commit)
	}
	if err := deps.Repo.RevertAbort(ctx, ""); err != nil {
		return Result{}, fmt.Errorf("revert: abort failed: %w", err)
	}
	// Tree is back to the pre-revert tip; safe to restore the autostash.
	return op.restore(ctx, deps, stashed, Result{Summary: "aborted: revert " + op.Commit, Changed: false})
}

// restore pops the autostash (if any) onto the now-clean tree. A pop conflict
// surfaces like CherryPick's: the changes are preserved in the stash.
func (op Revert) restore(ctx context.Context, deps OpDeps, stashed bool, res Result) (Result, error) {
	if !stashed {
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "restoring changes"})
	if err := deps.Repo.StashPop(ctx, ""); err != nil {
		return Result{Summary: res.Summary + "; restore conflicted (changes preserved in stash)", Changed: res.Changed},
			fmt.Errorf("stash pop conflict after revert %s: %w", op.Commit, err)
	}
	return res, nil
}
