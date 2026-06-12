package engine

import (
	"context"
	"fmt"
)

// DeleteBranch deletes a local branch. Force is resolved reactively via the
// Decider — only when git refuses the safe `branch -d`.
type DeleteBranch struct {
	Name string // required
}

func (op DeleteBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("delete branch: Name is required")
	}

	// Guards: fail fast with a clear message before asking anything. git would
	// refuse both cases anyway, but only after a pointless confirm prompt.
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if cur == op.Name {
		return Result{}, fmt.Errorf("delete branch: cannot delete the checked-out branch %s — switch away first", op.Name)
	}
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, wt := range wts {
		if wt.Branch == op.Name {
			return Result{}, fmt.Errorf("delete branch: %s is checked out in worktree %s — remove the worktree first", op.Name, wt.Path)
		}
	}

	// Decision 1: confirm. A single TUI keypress must not delete a ref
	// unconfirmed; the CLI pre-answers this (the command is the confirmation).
	confirm, err := deps.decide(ctx, DecisionRequest{
		ID:      "delete-branch",
		Prompt:  "Delete branch " + op.Name + "?",
		Options: []string{"delete", "abort"},
	})
	if err != nil {
		return Result{}, err
	}
	if confirm.Option != "delete" {
		return Result{Summary: "cancelled", Changed: false}, nil
	}

	deps.emit(ctx, Progress{Step: "deleting branch", Detail: op.Name})

	// Safe delete first; force only via the same branch-unmerged fork
	// RemoveWorktree ships (one decision shape across frontends).
	if err := deps.Repo.DeleteBranch(ctx, op.Name, false); err != nil {
		choice, derr := deps.decide(ctx, DecisionRequest{
			ID:      "branch-unmerged",
			Prompt:  "Branch " + op.Name + " is not fully merged; force-delete discards its unmerged commits.",
			Options: []string{"force-delete", "keep"},
		})
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != "force-delete" {
			return Result{Summary: "kept branch " + op.Name, Changed: false}, nil
		}
		if err := deps.Repo.DeleteBranch(ctx, op.Name, true); err != nil {
			return Result{}, fmt.Errorf("delete branch (force): %w", err)
		}
	}

	res := Result{Summary: "deleted branch " + op.Name, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = DeleteBranch{}
