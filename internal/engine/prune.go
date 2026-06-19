package engine

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/repogate"
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
		return Result{Summary: "no remotes to prune"}, nil
	}
	deps.emit(ctx, Progress{Step: "pruning", Detail: strings.Join(names, " ")})
	if err := deps.Repo.PruneRemotes(ctx, names...); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pruned remotes: " + strings.Join(names, " "), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = Prune{}
