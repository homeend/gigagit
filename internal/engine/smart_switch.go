package engine

import (
	"context"
	"fmt"
)

// SmartSwitch checks out Branch, automatically stashing and restoring local
// changes. On a restore (stash pop) conflict it never drops the stash — git
// retains it — and surfaces the conflict.
type SmartSwitch struct{ Branch string }

func (op SmartSwitch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if cur == op.Branch {
		return Result{Summary: "already on " + op.Branch}, nil
	}

	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := deps.Repo.StashPush(ctx, "gg-autostash:"+op.Branch, nil, false); err != nil {
			return Result{}, err
		}
		stashed = true
	}

	deps.emit(ctx, Progress{Step: "switching", Detail: op.Branch})
	if err := deps.Repo.Switch(ctx, op.Branch); err != nil {
		if stashed {
			_ = deps.Repo.StashPop(ctx, "") // best-effort restore on the original branch
		}
		return Result{}, err
	}

	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := deps.Repo.StashPop(ctx, ""); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: DecisionRequest{
				ID:      "stash-pop-conflict",
				Prompt:  "Restoring your changes conflicts with " + op.Branch,
				Options: []string{"keep", "abort"},
			}})
			return Result{Summary: "switched to " + op.Branch + "; restore conflicted (changes preserved in stash)", Changed: true},
				fmt.Errorf("stash pop conflict after switching to %s: %w", op.Branch, err)
		}
	}
	return Result{Summary: "switched to " + op.Branch, Changed: true}, nil
}

var _ Operation = SmartSwitch{}
