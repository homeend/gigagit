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
		return Result{}.WithSummary("already on %s", op.Branch), nil
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

	res := Result{Changed: true}.WithSummary("switched to %s", op.Branch)
	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := deps.Repo.StashPop(ctx, ""); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: PromptReq("stash-pop-conflict", "Restoring your changes conflicts with %s", []string{"keep", "abort"}, op.Branch)})
			return res.AppendSummary("; restore conflicted (changes preserved in stash)"),
				fmt.Errorf("stash pop conflict after switching to %s: %w", op.Branch, err)
		}
	}
	return res, nil
}

var _ Operation = SmartSwitch{}
