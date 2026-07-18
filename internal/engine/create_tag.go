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
	Force   bool   // replace existing tag
}

func (op CreateTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("create tag: Name is required")
	}
	if op.Commit != "" {
		deps.emit(ctx, Progressf("creating tag", "%s at %s", op.Name, op.Commit))
	} else {
		deps.emit(ctx, Progress{Step: "creating tag", Detail: op.Name})
	}

	if err := deps.Repo.CreateTag(ctx, op.Name, op.Commit, op.Message, op.Force); err != nil {
		return Result{}, fmt.Errorf("create tag: %w", err)
	}
	var res Result
	if op.Message != "" {
		res = Result{Changed: true}.WithSummary("created annotated tag %s", op.Name)
	} else {
		res = Result{Changed: true}.WithSummary("created lightweight tag %s", op.Name)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CreateTag{}
