package engine

import (
	"context"
	"fmt"
)

// CreateBranch creates a new local branch without switching to it.
type CreateBranch struct {
	Name       string // required
	StartPoint string // "" = HEAD
}

func (op CreateBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("create branch: Name is required")
	}

	// Validate up front so an illegal name fails with a clear message instead
	// of git's terser ref error.
	if err := deps.Repo.CheckRefFormatBranch(ctx, op.Name); err != nil {
		return Result{}, fmt.Errorf("create branch: invalid branch name %q: %w", op.Name, err)
	}

	detail := op.Name
	if op.StartPoint != "" {
		detail += " from " + op.StartPoint
	}
	deps.emit(ctx, Progress{Step: "creating branch", Detail: detail})

	// An already-existing branch is refused by git itself; just wrap the error.
	if err := deps.Repo.CreateBranch(ctx, op.Name, op.StartPoint); err != nil {
		return Result{}, fmt.Errorf("create branch: %w", err)
	}

	res := Result{Summary: "created branch " + op.Name, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CreateBranch{}
