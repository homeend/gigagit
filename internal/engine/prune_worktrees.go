package engine

import (
	"context"

	"github.com/homeend/gigagit/internal/repogate"
)

// PruneWorktrees removes stale $GIT_DIR/worktrees administrative entries
// (git worktree prune). Default TreeWrite reservation: it mutates .git
// admin state. Not dispatched by the TUI, so no opAffectedSources mapping
// is needed.
type PruneWorktrees struct{}

var _ Operation = PruneWorktrees{}

func (op PruneWorktrees) LockMode() repogate.Mode { return repogate.TreeWrite }

func (op PruneWorktrees) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "pruning worktrees"})
	if err := deps.Repo.PruneWorktrees(ctx); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pruned stale worktrees", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
