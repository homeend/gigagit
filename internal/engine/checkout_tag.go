package engine

import (
	"context"
	"fmt"
)

// CheckoutTag checks out a tag: detached at the tag's commit (Branch == "") or by
// creating Branch at the tag and switching to it. Decision-free; the frontend
// resolves the detached-vs-branch fork (and any branch name) before calling, per
// the option-list-only decision contract (a branch name is free text).
type CheckoutTag struct {
	Name   string // tag (required)
	Branch string // "" = detached HEAD; else new branch created at the tag
}

func (op CheckoutTag) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("checkout tag: Name is required")
	}
	if op.Branch != "" {
		deps.emit(ctx, Progress{Step: "creating branch", Detail: op.Branch + " at " + op.Name})
		// One atomic invocation: on failure no branch is left behind.
		if err := deps.Repo.SwitchCreate(ctx, op.Branch, op.Name); err != nil {
			return Result{}, fmt.Errorf("checkout tag: %w", err)
		}
		res := Result{Summary: "created branch " + op.Branch + " at " + op.Name + " and switched", Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "checking out", Detail: op.Name})
	if err := deps.Repo.SwitchDetach(ctx, op.Name); err != nil {
		return Result{}, fmt.Errorf("checkout tag: %w", err)
	}
	res := Result{Summary: "checked out " + op.Name + " (detached HEAD)", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CheckoutTag{}
