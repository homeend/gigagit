package engine

import (
	"context"
	"fmt"
)

// RenameBranch renames a local branch (git branch -m). Mirrors CreateBranch:
// the new name is format-validated up front for a clear message; git itself
// refuses an existing target.
type RenameBranch struct {
	Old string // required
	New string // required
}

func (op RenameBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Old == "" || op.New == "" {
		return Result{}, fmt.Errorf("rename branch: Old and New are required")
	}
	if err := deps.Repo.CheckRefFormatBranch(ctx, op.New); err != nil {
		return Result{}, fmt.Errorf("rename branch: invalid branch name %q: %w", op.New, err)
	}
	deps.emit(ctx, Progress{Step: "renaming branch", Detail: op.Old + " → " + op.New})
	if err := deps.Repo.RenameBranch(ctx, op.Old, op.New); err != nil {
		return Result{}, fmt.Errorf("rename branch: %w", err)
	}
	res := Result{Changed: true}.WithSummary("renamed branch %s → %s", op.Old, op.New)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = RenameBranch{}
