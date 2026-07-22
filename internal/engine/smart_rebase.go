package engine

import (
	"context"
	"fmt"
)

// SmartRebase replays Branch's commits onto Onto, picking the simplest correct
// path: in place when Branch is checked out here, inside the worktree that has
// Branch checked out (you stay put), else autostash + switch + rebase, ending
// on Branch. Unlike merge it REWRITES Branch, so the ladder pivots on Branch
// (the moving branch), not Onto. A conflict pauses the replay mid-flight
// (detached HEAD) and forks via the "rebase-conflict" decision: keep-conflicts
// leaves the paused rebase for `git rebase --continue` (the op returns an
// error), abort runs `git rebase --abort`.
type SmartRebase struct {
	Branch string
	Onto   string
}

var _ Operation = SmartRebase{}

func (op SmartRebase) Run(ctx context.Context, deps OpDeps) (Result, error) {
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	branch := op.Branch
	if branch == "" {
		if cur == "" {
			return Result{}, fmt.Errorf("smart rebase: detached HEAD — specify a branch to rebase")
		}
		branch = cur
	}
	if op.Onto == "" {
		return Result{}, fmt.Errorf("smart rebase: Onto is required")
	}
	if branch == op.Onto {
		return Result{}, fmt.Errorf("smart rebase: branch and base are both %s", branch)
	}
	branches, err := deps.Repo.Branches(ctx)
	if err != nil {
		return Result{}, err
	}
	have := make(map[string]bool, len(branches))
	for _, b := range branches {
		have[b.Name] = true
	}
	// Branch must be a local branch (it gets rewritten + checked out). Onto may be
	// a branch OR any resolvable commit-ish — a tag, a remote-tracking ref, a SHA —
	// (mirrors InteractiveRebase's commit-ish Onto).
	if !have[branch] {
		return Result{}, fmt.Errorf("smart rebase: no such branch: %s", branch)
	}
	if ok, err := deps.Repo.CommitExists(ctx, op.Onto); err != nil {
		return Result{}, err
	} else if !ok {
		return Result{}, fmt.Errorf("smart rebase: no such commit: %s", op.Onto)
	}

	snapshotBranchTip(ctx, deps, branch, "rebase")

	// Rung 1: Branch is checked out right here.
	if branch == cur {
		return op.rebaseAt(ctx, deps, "", branch)
	}

	// Rung 2: Branch lives in another worktree — rebase there, stay put.
	wt, err := deps.Repo.WorktreeForBranch(ctx, branch)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		return op.rebaseAt(ctx, deps, wt.Path, branch)
	}

	// Rung 3: autostash if dirty, switch to Branch, rebase, stay on Branch.
	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := deps.Repo.StashPush(ctx, "gg-autostash:"+branch, nil, false); err != nil {
			return Result{}, err
		}
		stashed = true
	}
	deps.emit(ctx, Progress{Step: "switching", Detail: branch})
	if err := deps.Repo.Switch(ctx, branch); err != nil {
		if stashed {
			_ = deps.Repo.StashPop(ctx, "") // best-effort restore on the original branch
		}
		return Result{}, err
	}

	res, rebaseErr := op.rebaseAt(ctx, deps, "", branch)
	if rebaseErr != nil {
		// Paused mid-rebase (or refused outright): popping onto that tree would
		// compound the mess. The stash survives.
		if stashed && res.Summary != "" {
			res = res.AppendSummary(" (your changes remain stashed)")
		}
		return res, rebaseErr
	}
	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := deps.Repo.StashPop(ctx, ""); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: PromptReq("stash-pop-conflict", "Restoring your changes conflicted", []string{"keep", "abort"})})
			return res.AppendSummary("; restore conflicted (changes preserved in stash)"),
				fmt.Errorf("stash pop conflict after rebasing %s: %w", branch, err)
		}
	}
	return res, nil
}

// rebaseAt rebases branch onto op.Onto inside dir ("" = the current worktree),
// resolving a conflict via the rebase-conflict decision. A kept conflict leaves
// the repo paused mid-replay (detached HEAD), NOT cleanly on branch.
func (op SmartRebase) rebaseAt(ctx context.Context, deps OpDeps, dir, branch string) (Result, error) {
	if dir == "" {
		deps.emit(ctx, Progressf("rebasing", "%s onto %s", branch, op.Onto))
	} else {
		deps.emit(ctx, Progressf("rebasing", "%s onto %s in worktree %s", branch, op.Onto, dir))
	}
	rebaseErr := deps.Repo.Rebase(ctx, dir, op.Onto)
	if rebaseErr == nil {
		res := Result{Changed: true}.WithSummary("rebased %s onto %s", branch, op.Onto)
		if dir != "" {
			res = res.AppendSummary(" in worktree %s", dir)
		}
		return res, nil
	}
	inRebase, stateErr := deps.Repo.RebaseInProgress(ctx, dir)
	if stateErr != nil {
		return Result{}, fmt.Errorf("smart rebase: %s onto %s: %v (state check: %w)", branch, op.Onto, rebaseErr, stateErr)
	}
	if !inRebase {
		// Refused outright (e.g. nothing to replay): nothing to resolve.
		return Result{}, fmt.Errorf("smart rebase: %s onto %s: %w", branch, op.Onto, rebaseErr)
	}
	choice, derr := deps.decide(ctx, PromptReq("rebase-conflict", "Rebasing %s onto %s hit conflicts", []string{"keep-conflicts", "abort"}, branch, op.Onto))
	if derr != nil {
		return Result{}, derr
	}
	if choice.Option == "keep-conflicts" {
		res := Result{Changed: true}.WithSummary("rebase of %s onto %s", branch, op.Onto)
		if dir != "" {
			res = res.AppendSummary(" in worktree %s", dir)
		}
		res = res.AppendSummary(" paused on a conflict (resolve, then `git rebase --continue`)")
		return res, fmt.Errorf("rebase conflict: %s onto %s", branch, op.Onto)
	}
	if err := deps.Repo.RebaseAbort(ctx, dir); err != nil {
		return Result{}, fmt.Errorf("smart rebase: abort failed: %w", err)
	}
	return Result{Changed: false}.WithSummary("aborted: rebasing %s onto %s", branch, op.Onto), nil
}
