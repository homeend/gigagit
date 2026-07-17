package engine

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/repogate"
)

// Prune drops tracking refs for branches deleted upstream, across all remotes.
// RefWrite: refs only.
type Prune struct{}

func (op Prune) LockMode() repogate.Mode { return repogate.RefWrite }

func (op Prune) Run(ctx context.Context, deps OpDeps) (Result, error) {
	names, err := deps.Repo.RemoteNames(ctx)
	if err != nil {
		return Result{}, err
	}
	if len(names) == 0 {
		return Result{}.WithSummary("no remotes to prune"), nil
	}
	deps.emit(ctx, Progress{Step: "pruning", Detail: strings.Join(names, " ")})
	if err := deps.Repo.PruneRemotes(ctx, names...); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("pruned remotes: %s", strings.Join(names, " "))
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = Prune{}
