package engine

import (
	"context"
	"fmt"
)

// SmartMerge merges Source into Target (default: the current branch), picking
// the simplest correct path: in place when Target is checked out here, inside
// the worktree that has Target checked out (you stay put), else autostash +
// switch + merge, ending on Target. A conflicted merge forks via the
// "merge-conflict" decision: keep-conflicts leaves the tree for manual
// resolution (the op returns an error), abort runs `git merge --abort`.
type SmartMerge struct {
	Source string
	Target string
}

var _ Operation = SmartMerge{}

func (op SmartMerge) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Source == "" {
		return Result{}, fmt.Errorf("smart merge: Source is required")
	}
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	target := op.Target
	if target == "" {
		if cur == "" {
			return Result{}, fmt.Errorf("smart merge: detached HEAD — specify a target branch")
		}
		target = cur
	}
	if op.Source == target {
		return Result{}, fmt.Errorf("smart merge: source and target are both %s", target)
	}
	branches, err := deps.Repo.Branches(ctx)
	if err != nil {
		return Result{}, err
	}
	have := make(map[string]bool, len(branches))
	for _, b := range branches {
		have[b.Name] = true
	}
	// Target must be a local branch (we end up checked out on it). Source may be a
	// branch OR any resolvable commit-ish — a tag, a remote-tracking ref, a SHA —
	// since `git merge <source>` accepts any of them (mirrors InteractiveRebase's
	// commit-ish Onto).
	if !have[target] {
		return Result{}, fmt.Errorf("smart merge: no such branch: %s", target)
	}
	if ok, err := deps.Repo.CommitExists(ctx, op.Source); err != nil {
		return Result{}, err
	} else if !ok {
		return Result{}, fmt.Errorf("smart merge: no such commit: %s", op.Source)
	}

	// Rung 1: Target is checked out right here.
	if target == cur {
		return op.mergeAt(ctx, deps, "", target)
	}

	// Rung 2: Target lives in another worktree — merge there, stay put.
	wt, err := deps.Repo.WorktreeForBranch(ctx, target)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		return op.mergeAt(ctx, deps, wt.Path, target)
	}

	// Rung 3: autostash if dirty, switch to Target, merge, stay on Target.
	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := deps.Repo.StashPush(ctx, "gg-autostash:"+target, nil, false); err != nil {
			return Result{}, err
		}
		stashed = true
	}
	deps.emit(ctx, Progress{Step: "switching", Detail: target})
	if err := deps.Repo.Switch(ctx, target); err != nil {
		if stashed {
			_ = deps.Repo.StashPop(ctx, "") // best-effort restore on the original branch
		}
		return Result{}, err
	}

	res, mergeErr := op.mergeAt(ctx, deps, "", target)
	if mergeErr != nil {
		// Conflicts kept (or the merge failed outright): popping the stash onto
		// that tree would compound the mess. The stash survives.
		if stashed && res.Summary != "" {
			res = res.AppendSummary(" (your changes remain stashed)")
		}
		return res, mergeErr
	}
	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := deps.Repo.StashPop(ctx, ""); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: PromptReq("stash-pop-conflict", "Restoring your changes conflicted", []string{"keep", "abort"})})
			return res.AppendSummary("; restore conflicted (changes preserved in stash)"),
				fmt.Errorf("stash pop conflict after merging into %s: %w", target, err)
		}
	}
	return res, nil
}

// mergeAt merges op.Source into target inside dir ("" = the current
// worktree), resolving a conflict via the merge-conflict decision.
func (op SmartMerge) mergeAt(ctx context.Context, deps OpDeps, dir, target string) (Result, error) {
	if dir == "" {
		deps.emit(ctx, Progressf("merging", "%s into %s", op.Source, target))
	} else {
		deps.emit(ctx, Progressf("merging", "%s into %s in worktree %s", op.Source, target, dir))
	}
	mergeErr := deps.Repo.Merge(ctx, dir, op.Source)
	if mergeErr == nil {
		res := Result{Changed: true}.WithSummary("merged %s into %s", op.Source, target)
		if dir != "" {
			res = res.AppendSummary(" in worktree %s", dir)
		}
		return res, nil
	}
	inMerge, stateErr := deps.Repo.MergeInProgress(ctx, dir)
	if stateErr != nil {
		return Result{}, fmt.Errorf("smart merge: %s into %s: %v (state check: %w)", op.Source, target, mergeErr, stateErr)
	}
	if !inMerge {
		// Refused outright (e.g. unrelated histories): nothing to resolve.
		return Result{}, fmt.Errorf("smart merge: %s into %s: %w", op.Source, target, mergeErr)
	}
	choice, derr := deps.decide(ctx, PromptReq("merge-conflict", "Merging %s into %s hit conflicts", []string{"keep-conflicts", "abort"}, op.Source, target))
	if derr != nil {
		return Result{}, derr
	}
	if choice.Option == "keep-conflicts" {
		res := Result{Changed: true}.WithSummary("merge of %s into %s", op.Source, target)
		if dir != "" {
			res = res.AppendSummary(" in worktree %s", dir)
		}
		res = res.AppendSummary(" has conflicts (left in tree)")
		return res, fmt.Errorf("merge conflict: %s into %s", op.Source, target)
	}
	if err := deps.Repo.MergeAbort(ctx, dir); err != nil {
		return Result{}, fmt.Errorf("smart merge: abort failed: %w", err)
	}
	return Result{Changed: false}.WithSummary("aborted: merging %s into %s", op.Source, target), nil
}
