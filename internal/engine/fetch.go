package engine

import (
	"context"

	"github.com/homeend/gigagit/internal/repogate"
)

// Fetch updates tracking refs for every configured remote. RefWrite: it changes
// refs, not the working tree.
type Fetch struct{}

func (op Fetch) LockMode() repogate.Mode { return repogate.RefWrite }

func (op Fetch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "fetching all remotes"})
	if err := deps.Repo.FetchAll(ctx); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("fetched all remotes")
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = Fetch{}
