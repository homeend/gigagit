package engine

import (
	"context"
	"fmt"
)

// Checkout checks out a commit-ish (tag, branch, or raw SHA): detached at Ref
// (Branch == "") or by creating Branch at Ref and switching to it. Decision-free;
// the frontend resolves the detached-vs-branch fork (and any branch name) before
// calling, per the option-list-only decision contract (a branch name is free text).
type Checkout struct {
	Ref    string // commit-ish (required)
	Branch string // "" = detached HEAD; else new branch created at Ref and switched to
}

func (op Checkout) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Ref == "" {
		return Result{}, fmt.Errorf("checkout: Ref is required")
	}
	if op.Branch != "" {
		deps.emit(ctx, Progress{Step: "creating branch", Detail: op.Branch + " at " + op.Ref})
		// One atomic invocation: on failure no branch is left behind.
		if err := deps.Repo.SwitchCreate(ctx, op.Branch, op.Ref); err != nil {
			return Result{}, fmt.Errorf("checkout: %w", err)
		}
		res := Result{Summary: "created branch " + op.Branch + " at " + op.Ref + " and switched", Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "checking out", Detail: op.Ref})
	if err := deps.Repo.SwitchDetach(ctx, op.Ref); err != nil {
		return Result{}, fmt.Errorf("checkout: %w", err)
	}
	res := Result{Summary: "checked out " + op.Ref + " (detached HEAD)", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = Checkout{}
