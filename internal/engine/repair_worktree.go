package engine

import (
	"context"
)

// RepairWorktree rebinds a linked worktree's two absolute-path link records
// (the admin gitdir file and the worktree's .git back-link) to the given
// on-disk path (git worktree repair <path>). Backs the TUI's
// cross-environment repair offer for a worktree recorded under the other
// environment's path notation (WSL vs Windows). Default TreeWrite
// reservation: it rewrites .git admin metadata. No decisions — the
// repair/cancel confirm is frontend-side.
type RepairWorktree struct {
	Path string // the worktree path as reachable from THIS environment
}

var _ Operation = RepairWorktree{}

func (op RepairWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "repairing worktree"})
	if err := deps.Repo.WorktreeRepair(ctx, op.Path); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("repaired worktree link: %s", op.Path)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
