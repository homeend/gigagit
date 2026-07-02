package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/repogate"
)

// WriteCommitGraph writes the repo's commit-graph file so ordered commit
// walks (the Commits panel's --date-order paging) go from O(walk) to
// near-flat. Decision-free; backs the notification center's commit-graph
// recommendation and the Settings "Commit-graph" row.
type WriteCommitGraph struct{}

// LockMode: the commit-graph is a derived cache under .git/objects/info —
// it touches neither refs nor the working tree.
func (op WriteCommitGraph) LockMode() repogate.Mode { return repogate.Read }

func (op WriteCommitGraph) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "writing commit-graph", Detail: "git commit-graph write --reachable"})
	err := deps.Repo.CommitGraphWrite(ctx, func(line string) {
		deps.emit(ctx, GitLine{Raw: line})
	})
	if err != nil {
		return Result{}, fmt.Errorf("write commit-graph: %w", err)
	}
	res := Result{Summary: "commit-graph written", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = WriteCommitGraph{}
