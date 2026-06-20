package engine

import (
	"context"
	"fmt"
)

// CreateTag creates a tag at Commit (empty = HEAD). A non-empty Message makes it
// annotated; otherwise lightweight. Decision-free: a duplicate name or bad ref
// surfaces as a git error.
type CreateTag struct {
	Name    string // required
	Commit  string // "" = HEAD
	Message string // "" = lightweight, else annotated
}

func (op CreateTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("create tag: Name is required")
	}
	detail := op.Name
	if op.Commit != "" {
		detail += " at " + op.Commit
	}
	deps.emit(ctx, Progress{Step: "creating tag", Detail: detail})

	if err := deps.Repo.CreateTag(ctx, op.Name, op.Commit, op.Message); err != nil {
		return Result{}, fmt.Errorf("create tag: %w", err)
	}
	kind := "lightweight"
	if op.Message != "" {
		kind = "annotated"
	}
	res := Result{Summary: "created " + kind + " tag " + op.Name, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CreateTag{}
