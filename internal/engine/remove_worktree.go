package engine

import (
	"context"
	"fmt"
	"path/filepath"
)

// RemoveWorktree removes a linked worktree at Path, optionally deleting its
// Branch. Force is resolved reactively via the Decider — only when git refuses
// the safe command. Branch is "" for a detached worktree (nothing to delete).
type RemoveWorktree struct {
	Path   string // absolute path of the worktree to remove
	Branch string // its short branch name; "" if detached
}

func (op RemoveWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Path == "" {
		return Result{}, fmt.Errorf("remove worktree: Path is required")
	}

	// Guard: never remove the worktree we're standing in, nor the primary one.
	// git refuses both anyway, but a clean up-front message avoids a pointless
	// "force?" prompt that would also fail.
	top, err := deps.Repo.TopLevel(ctx)
	if err != nil {
		return Result{}, err
	}
	if samePath(op.Path, top) {
		return Result{}, fmt.Errorf("remove worktree: cannot remove the worktree you are currently in (%s)", op.Path)
	}
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return Result{}, err
	}
	// `git worktree list` always lists the main (primary) worktree first.
	if len(wts) > 0 && samePath(op.Path, wts[0].Path) {
		return Result{}, fmt.Errorf("remove worktree: cannot remove the main worktree (%s)", op.Path)
	}

	// Decision 1: scope. A detached worktree has no branch to offer.
	scopeOpts := []string{"worktree-only", "abort"}
	if op.Branch != "" {
		scopeOpts = []string{"worktree-only", "worktree-and-branch", "abort"}
	}
	scope, err := deps.decide(ctx, DecisionRequest{
		ID:      "remove-scope",
		Prompt:  "Remove worktree at " + op.Path + "?",
		Options: scopeOpts,
	})
	if err != nil {
		return Result{}, err
	}
	if scope.Option == "abort" {
		return Result{Summary: "cancelled", Changed: false}, nil
	}

	// Step 2: remove the worktree (safe, then reactive force on any failure).
	deps.emit(ctx, Progress{Step: "removing worktree", Detail: op.Path})
	onLine := func(line string) { deps.emit(ctx, GitLine{Raw: line}) }
	if err := deps.Repo.RemoveWorktree(ctx, op.Path, false, onLine); err != nil {
		force, derr := deps.decide(ctx, DecisionRequest{
			ID:      "worktree-dirty",
			Prompt:  "Cannot remove " + op.Path + " cleanly (it may have uncommitted changes). Force?",
			Options: []string{"force", "abort"},
		})
		if derr != nil {
			return Result{}, derr
		}
		if force.Option != "force" {
			return Result{Summary: "cancelled; worktree not removed", Changed: false}, nil
		}
		if err := deps.Repo.RemoveWorktree(ctx, op.Path, true, onLine); err != nil {
			return Result{}, fmt.Errorf("remove worktree (force): %w", err)
		}
	}

	summary := "removed worktree " + op.Path

	// Step 3: delete the branch if requested. Must follow worktree removal —
	// git refuses to delete a branch still checked out in a worktree.
	if scope.Option == "worktree-and-branch" && op.Branch != "" {
		deps.emit(ctx, Progress{Step: "deleting branch", Detail: op.Branch})
		if err := deps.Repo.DeleteBranch(ctx, op.Branch, false); err != nil {
			choice, derr := deps.decide(ctx, DecisionRequest{
				ID:      "branch-unmerged",
				Prompt:  "Branch " + op.Branch + " is not fully merged; force-delete discards its unmerged commits.",
				Options: []string{"force-delete", "keep"},
			})
			if derr != nil {
				return Result{}, derr
			}
			if choice.Option == "force-delete" {
				if err := deps.Repo.DeleteBranch(ctx, op.Branch, true); err != nil {
					return Result{}, fmt.Errorf("delete branch (force): %w", err)
				}
				summary += " and branch " + op.Branch
			} else {
				summary += " (branch " + op.Branch + " kept)"
			}
		} else {
			summary += " and branch " + op.Branch
		}
	}

	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// samePath compares two paths after resolving symlinks (git's --show-toplevel
// may resolve symlinks while `worktree list` may not). Falls back to the raw
// string when a path cannot be resolved.
func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}

var _ Operation = RemoveWorktree{}
