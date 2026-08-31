package engine

import (
	"context"
	"fmt"
)

// FastForward advances a branch to Commit when Commit is a descendant of its
// tip (git merge --ff-only, or a same-repo ff-only fetch for a branch that is
// not checked out). It never rewrites history and never creates a merge
// commit; it refuses if the target is not strictly ahead. Non-destructive,
// so there is no Decider prompt.
//
// Branch selects the branch to advance; empty means the current branch (the
// Commits-panel lane, where Commit is typically a commit on a child branch
// built atop the current one). A named Branch that is checked out — here or
// in another worktree — advances via merge only when it is THIS worktree's
// current branch; git itself refuses the ref-update path for a branch checked
// out elsewhere, and that refusal surfaces as the op error.
type FastForward struct {
	Commit string
	Branch string
}

var _ Operation = FastForward{}

func (op FastForward) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Commit == "" {
		return Result{}, fmt.Errorf("fast-forward: Commit is required")
	}

	current, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	branch := op.Branch
	if branch == "" {
		if current == "" {
			return Result{}, fmt.Errorf("fast-forward needs a checked-out branch (HEAD is detached)")
		}
		branch = current
	}

	target, err := deps.Repo.RevParse(ctx, op.Commit)
	if err != nil {
		return Result{}, fmt.Errorf("fast-forward: %w", err)
	}
	tip, err := deps.Repo.RevParse(ctx, "refs/heads/"+branch)
	if err != nil {
		return Result{}, fmt.Errorf("fast-forward: %w", err)
	}
	if target == tip {
		return Result{Changed: false}.WithSummary("%s already up to date", branch), nil
	}

	ahead, err := deps.Repo.IsAncestor(ctx, tip, target)
	if err != nil {
		return Result{}, err
	}
	if !ahead {
		return Result{}, fmt.Errorf("cannot fast-forward %s: %s is not ahead of it", branch, shortSHA(target))
	}

	deps.emit(ctx, Progress{Step: "fast-forwarding", Detail: branch + " → " + shortSHA(target)})
	if branch == current {
		// The current branch: merge --ff-only moves the ref AND the working
		// tree together.
		if err := deps.Repo.MergeFFOnly(ctx, "", target); err != nil {
			return Result{}, fmt.Errorf("fast-forward %s to %s: %w", branch, shortSHA(target), err)
		}
	} else {
		// Not checked out here: a same-repo ff-only fetch moves the ref
		// without touching any working tree. Git refuses it for a branch
		// checked out in another worktree.
		if err := deps.Repo.FastForwardToRef(ctx, branch, target); err != nil {
			return Result{}, fmt.Errorf("fast-forward %s to %s: %w", branch, shortSHA(target), err)
		}
	}
	res := Result{Changed: true}.WithSummary("fast-forwarded %s to %s", branch, shortSHA(target))
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
