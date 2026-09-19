package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// FetchPRHead brings pull request Number's head commit into the gg-private
// ref refs/gg/pr/<Number>. Nothing reaches the forge; no branch, index or
// worktree is touched. HeadSHA (when known) makes it idempotent: a ref already
// at that commit costs one rev-parse and no network.
type FetchPRHead struct {
	Remote  string // configured remote name or URL of the BASE repository
	Refspec string // server-side ref of the PR head (the provider's HeadRefspec)
	Number  int
	HeadSHA string // optional
}

var _ Operation = FetchPRHead{}

// LockMode: writes one private ref; never index/worktree/HEAD.
func (op FetchPRHead) LockMode() repogate.Mode { return repogate.RefWrite }

func (op FetchPRHead) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Number <= 0 || op.Remote == "" || op.Refspec == "" {
		return Result{}, fmt.Errorf("fetch pr head: number, remote and refspec are required")
	}
	ref := git.PRRef(op.Number)
	if op.HeadSHA != "" {
		if cur, err := deps.Repo.RevParse(ctx, ref+"^{commit}"); err == nil && cur == op.HeadSHA {
			res := Result{}.WithSummary("pull request #%d is already fetched", op.Number)
			deps.emit(ctx, Done{Result: res})
			return res, nil
		}
	}
	deps.emit(ctx, Progress{Step: "fetching pull request head", Detail: fmt.Sprintf("#%d", op.Number)})
	if err := deps.Repo.FetchRefspec(ctx, op.Remote, op.Refspec, ref); err != nil {
		return Result{}, fmt.Errorf("fetch pr head #%d: %w", op.Number, err)
	}
	res := Result{Changed: true}.WithSummary("fetched pull request #%d", op.Number)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
