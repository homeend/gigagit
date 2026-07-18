package engine

import (
	"context"
	"fmt"
)

// DeleteTag deletes a tag. Decision-free: a missing tag surfaces as a git error.
type DeleteTag struct{ Name string }

func (op DeleteTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("delete tag: Name is required")
	}
	deps.emit(ctx, Progress{Step: "deleting tag", Detail: op.Name})
	if err := deps.Repo.DeleteTag(ctx, op.Name); err != nil {
		return Result{}, fmt.Errorf("delete tag: %w", err)
	}
	res := Result{Changed: true}.WithSummary("deleted tag %s", op.Name)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = DeleteTag{}
