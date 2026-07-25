package engine

import (
	"context"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// AddFetchMappings writes a per-branch fetch-refspec mapping for each named
// branch (skipping ones already present) and then fetches exactly those
// branches, so their remote-tracking refs materialize and the ↓↑ tip markers
// / ahead-behind start working. The notification center's "narrowed fetch
// refspec" fix action. Empty Branches is a no-op (the PushTags precedent).
// Deliberately never widens to the wildcard refspec.
type AddFetchMappings struct {
	Remote   string
	Branches []string
}

var _ Operation = AddFetchMappings{}

// LockMode: writes remote-tracking refs + a config line; never index/
// worktree/HEAD.
func (op AddFetchMappings) LockMode() repogate.Mode { return repogate.RefWrite }

func (op AddFetchMappings) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if len(op.Branches) == 0 {
		return Result{Changed: false}.WithSummary("no branches to map"), nil
	}
	key := "remote." + op.Remote + ".fetch"
	have, err := deps.Repo.ConfigGetAll(ctx, key)
	if err != nil {
		return Result{}, err
	}
	for _, b := range op.Branches {
		spec := fetchSpec(op.Remote, b)
		if slices.Contains(have, spec) {
			continue // idempotent re-run (e.g. after a failed fetch)
		}
		if err := deps.Repo.ConfigAdd(ctx, git.ConfigLocal, key, spec); err != nil {
			return Result{}, err
		}
	}
	deps.emit(ctx, Progress{Step: "fetching", Detail: op.Remote + " " + strings.Join(op.Branches, " ")})
	if err := deps.Repo.FetchBranches(ctx, op.Remote, op.Branches); err != nil {
		return Result{}, err // fetching is the op's purpose; config lines stay, re-run is idempotent
	}
	var res Result
	if len(op.Branches) == 1 {
		res = Result{Changed: true}.WithSummary("mapped 1 branch for tracking")
	} else {
		res = Result{Changed: true}.WithSummary("mapped %d branches for tracking", len(op.Branches))
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
