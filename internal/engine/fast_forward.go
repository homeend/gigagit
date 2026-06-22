package engine

import (
	"context"
	"fmt"
)

// FastForward advances the current branch to Commit when Commit is a descendant
// of HEAD (git merge --ff-only). It never rewrites history and never creates a
// merge commit; it refuses if the target is not strictly ahead. Non-destructive,
// so there is no Decider prompt. The Commits panel is the multi-branch feed, so
// Commit is typically a commit on a child branch built atop the current one.
type FastForward struct {
	Commit string
}

var _ Operation = FastForward{}

func (op FastForward) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Commit == "" {
		return Result{}, fmt.Errorf("fast-forward: Commit is required")
	}

	branch, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if branch == "" {
		return Result{}, fmt.Errorf("fast-forward needs a checked-out branch (HEAD is detached)")
	}

	target, err := deps.Repo.RevParse(ctx, op.Commit)
	if err != nil {
		return Result{}, fmt.Errorf("fast-forward: %w", err)
	}
	head, err := deps.Repo.RevParse(ctx, "HEAD")
	if err != nil {
		return Result{}, err
	}
	if target == head {
		return Result{Summary: branch + " already up to date", Changed: false}, nil
	}

	ahead, err := deps.Repo.IsAncestor(ctx, "HEAD", target)
	if err != nil {
		return Result{}, err
	}
	if !ahead {
		return Result{}, fmt.Errorf("cannot fast-forward %s: %s is not ahead of it", branch, shortSHA(target))
	}

	deps.emit(ctx, Progress{Step: "fast-forwarding", Detail: branch + " → " + shortSHA(target)})
	if err := deps.Repo.MergeFFOnly(ctx, "", target); err != nil {
		return Result{}, fmt.Errorf("fast-forward %s to %s: %w", branch, shortSHA(target), err)
	}
	res := Result{Summary: "fast-forwarded " + branch + " to " + shortSHA(target), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
