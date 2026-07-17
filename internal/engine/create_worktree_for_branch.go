package engine

import (
	"context"
	"fmt"
)

// CreateWorktreeForBranch creates a linked worktree that checks out an
// EXISTING branch (no new branch). A relative Path resolves against the
// repository root.
type CreateWorktreeForBranch struct {
	Branch         string
	Path           string
	PostCreateHook string // shell script run in the new worktree; "" = none
}

func (op CreateWorktreeForBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Branch == "" || op.Path == "" {
		return Result{}, fmt.Errorf("create worktree: Branch and Path are required")
	}

	// Guard: the branch must exist locally. Checking up front both gives a
	// clear message and forecloses git's remote-DWIM (a missing local branch
	// with a matching origin/<branch> would be silently created).
	branches, err := deps.Repo.Branches(ctx)
	if err != nil {
		return Result{}, err
	}
	found := false
	for _, b := range branches {
		if b.Name == op.Branch {
			found = true
			break
		}
	}
	if !found {
		return Result{}, fmt.Errorf("create worktree: no local branch %q", op.Branch)
	}

	// Guard: a branch can only be checked out in one worktree.
	wt, err := deps.Repo.WorktreeForBranch(ctx, op.Branch)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		return Result{}, fmt.Errorf("create worktree: branch %s is already checked out in worktree %s", op.Branch, wt.Path)
	}

	abs, err := resolveNewWorktreePath(ctx, deps, op.Path)
	if err != nil {
		return Result{}, err
	}

	deps.emit(ctx, Progress{Step: "creating worktree", Detail: op.Branch + " → " + abs})
	if err := deps.Repo.AddWorktreeForBranch(ctx, abs, op.Branch, func(line string) {
		deps.emit(ctx, GitLine{Raw: line})
	}); err != nil {
		return Result{}, fmt.Errorf("create worktree: %w", err)
	}

	res := runPostCreateHook(ctx, deps, Result{Changed: true, Path: abs}.WithSummary("worktree created: %s", abs), abs, op.Branch, op.PostCreateHook)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CreateWorktreeForBranch{}
