package engine

import (
	"context"
	"fmt"

	"github.com/gigagit/gg/internal/git"
)

// PullIntent expresses what the user wants from a pull of a non-current branch.
type PullIntent int

const (
	// PullAndStay ends with the target branch checked out.
	PullAndStay PullIntent = iota
	// PullInBackground updates the target's ref without leaving the current branch.
	PullInBackground
)

// SmartPull picks the simplest correct path to update Branch (default: current),
// per the design's decision tree.
type SmartPull struct {
	Branch string
	Remote string
	Intent PullIntent
}

var _ Operation = SmartPull{}

func (op SmartPull) Run(ctx context.Context, deps OpDeps) (Result, error) {
	repo := deps.Repo
	cur, err := repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	target := op.Branch
	if target == "" {
		target = cur
	}
	if target == "" {
		return Result{}, fmt.Errorf("smart pull: no target branch (detached HEAD)")
	}
	remote := op.Remote
	if remote == "" {
		remote, err = repo.RemoteForBranch(ctx, target)
		if err != nil {
			return Result{}, err
		}
	}

	if target == cur {
		return op.pullCurrent(ctx, deps, remote, target)
	}

	if op.Intent == PullInBackground {
		deps.emit(ctx, Progress{Step: "fast-forwarding ref", Detail: target})
		if err := repo.FastForwardRef(ctx, remote, target); err == nil {
			return Result{Summary: "fast-forwarded " + target, Changed: true}, nil
		}
		resp, derr := deps.decide(ctx, DecisionRequest{
			ID:      "not-fast-forwardable",
			Prompt:  "Cannot fast-forward " + target + " in the background",
			Options: []string{"checkout-and-resolve", "abort"},
		})
		if derr != nil {
			return Result{}, derr
		}
		if resp.Option == "checkout-and-resolve" {
			return op.checkoutPull(ctx, deps, remote, target, cur)
		}
		return Result{Summary: "aborted: " + target + " not fast-forwardable"}, nil
	}

	return op.checkoutPull(ctx, deps, remote, target, "")
}

func (op SmartPull) pullCurrent(ctx context.Context, deps OpDeps, remote, branch string) (Result, error) {
	deps.emit(ctx, Progress{Step: "fetching", Detail: remote})
	if err := deps.Repo.Fetch(ctx, remote); err != nil {
		return Result{}, err
	}
	deps.emit(ctx, Progress{Step: "pulling (ff-only)", Detail: branch})
	if err := deps.Repo.Pull(ctx, remote, branch, git.PullFF); err == nil {
		return Result{Summary: "pulled " + branch, Changed: true}, nil
	}
	resp, derr := deps.decide(ctx, DecisionRequest{
		ID:      "non-fast-forward",
		Prompt:  branch + " has diverged from " + remote,
		Options: []string{"rebase", "merge", "abort"},
	})
	if derr != nil {
		return Result{}, derr
	}
	switch resp.Option {
	case "rebase":
		if err := deps.Repo.Pull(ctx, remote, branch, git.PullRebase); err != nil {
			return Result{}, err
		}
		return Result{Summary: "pulled (rebased) " + branch, Changed: true}, nil
	case "merge":
		if err := deps.Repo.Pull(ctx, remote, branch, git.PullMerge); err != nil {
			return Result{}, err
		}
		return Result{Summary: "pulled (merged) " + branch, Changed: true}, nil
	default:
		return Result{Summary: "aborted: " + branch + " diverged"}, nil
	}
}

func (op SmartPull) checkoutPull(ctx context.Context, deps OpDeps, remote, target, returnTo string) (Result, error) {
	repo := deps.Repo

	wt, err := repo.WorktreeForBranch(ctx, target)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		deps.emit(ctx, Progress{Step: "pulling in worktree", Detail: wt.Path})
		if err := repo.PullInWorktree(ctx, wt.Path, remote, target); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: DecisionRequest{
				ID:      "worktree-pull-failed",
				Prompt:  "Pull in worktree " + wt.Path + " failed",
				Options: []string{"abort"},
			}})
			return Result{}, fmt.Errorf("smart pull: worktree %s: %w", wt.Path, err)
		}
		return Result{Summary: "pulled " + target + " in worktree " + wt.Path, Changed: true}, nil
	}

	dirty, err := repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := repo.StashPush(ctx, "gg-autostash:"+target); err != nil {
			return Result{}, err
		}
		stashed = true
	}

	deps.emit(ctx, Progress{Step: "switching", Detail: target})
	if err := repo.Switch(ctx, target); err != nil {
		if stashed {
			_ = repo.StashPop(ctx)
		}
		return Result{}, err
	}

	deps.emit(ctx, Progress{Step: "fetching", Detail: remote})
	_ = repo.Fetch(ctx, remote)
	deps.emit(ctx, Progress{Step: "pulling (ff-only)", Detail: target})
	pullErr := repo.Pull(ctx, remote, target, git.PullFF)

	if returnTo != "" && returnTo != target {
		deps.emit(ctx, Progress{Step: "switching back", Detail: returnTo})
		if err := repo.Switch(ctx, returnTo); err != nil {
			return Result{}, err
		}
	}

	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := repo.StashPop(ctx); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: DecisionRequest{
				ID:      "stash-pop-conflict",
				Prompt:  "Restoring your changes conflicted",
				Options: []string{"keep", "abort"},
			}})
			return Result{Summary: "pulled " + target + "; restore conflicted (changes preserved in stash)", Changed: true},
				fmt.Errorf("stash pop conflict after pulling %s: %w", target, err)
		}
	}
	if pullErr != nil {
		return Result{}, fmt.Errorf("smart pull: %s: %w", target, pullErr)
	}
	return Result{Summary: "pulled " + target, Changed: true}, nil
}
