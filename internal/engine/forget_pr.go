package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// ForgetPR drops gg's local record of a pull request: its refs/gg/pr/<n> ref.
// The ref name is built here from the number, so this op can never delete
// anything outside the PR namespace.
type ForgetPR struct{ Number int }

var _ Operation = ForgetPR{}

// LockMode: removes one private ref; never index/worktree/HEAD.
func (op ForgetPR) LockMode() repogate.Mode { return repogate.RefWrite }

func (op ForgetPR) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Number <= 0 {
		return Result{}, fmt.Errorf("forget pr: a pull request number is required")
	}
	ref := git.PRRef(op.Number)
	if _, err := deps.Repo.RevParse(ctx, ref+"^{commit}"); err != nil {
		res := Result{}.WithSummary("pull request #%d was not fetched", op.Number)
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	if err := deps.Repo.DeleteRef(ctx, ref); err != nil {
		return Result{}, fmt.Errorf("forget pr #%d: %w", op.Number, err)
	}
	res := Result{Changed: true}.WithSummary("forgot pull request #%d", op.Number)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
