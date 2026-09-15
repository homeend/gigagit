package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
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

// LockMode declares the gate reservation SmartPull needs: a background
// fast-forward moves only refs; every other path may touch the worktree.
func (op SmartPull) LockMode() repogate.Mode {
	if op.Intent == PullInBackground {
		return repogate.RefWrite
	}
	return repogate.TreeWrite
}

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
		// If the target is checked out in a worktree, git refuses to fetch into its
		// ref, so the ff-ref path below can't be tried — pull in that worktree
		// directly instead (no checkout, no switch-back, no spurious
		// not-fast-forwardable prompt). checkoutPull's worktree branch returns
		// before any switch, so returnTo is irrelevant. PullInWorktree touches a
		// tree, so escalate first — nothing partial has happened yet.
		wt, werr := repo.WorktreeForBranch(ctx, target)
		if werr != nil {
			return Result{}, werr
		}
		if wt != nil {
			if err := deps.escalate(ctx); err != nil {
				return Result{}, err
			}
			return op.checkoutPull(ctx, deps, remote, target, "")
		}
		// ONE fetch on this path. FastForwardRef IS a fetch (`git fetch
		// <remote> <branch>:<branch>`); the plain fetch that used to precede it
		// existed only so the snapshot could read a fresh
		// refs/remotes/<remote>/<branch> as Other. It never needed to: once
		// FastForwardRef succeeds the branch tip IS the upstream tip this pull
		// landed on, so Other is simply the post-fetch LOCAL tip and the
		// remote-tracking ref — which an explicit refspec leaves to chance — is
		// out of the picture entirely. On a ~100GB monorepo with background
		// auto-pull, halving the round-trips per branch is the whole point.
		// (git still updates refs/remotes/<remote>/<branch> opportunistically
		// under a default refspec, so the usual ahead/behind markers stay
		// honest; what is gone is the incidental refresh of every OTHER
		// branch's tracking ref, which a one-branch pull never promised.)
		//
		// The order inverts accordingly: capture Ours BEFORE the fetch (no
		// network), fetch, then record with the branch already moved — which is
		// why this is the one snapshotBranchTipAt call site.
		ours := tipOf(ctx, deps, target)
		deps.emit(ctx, Progress{Step: "fast-forwarding ref", Detail: target})
		if err := repo.FastForwardRef(ctx, remote, target); err == nil {
			// Ours doubles as the snapshotted tip: this path records the state
			// the fast-forward replaced. Target keeps the remote-tracking NAME
			// for labelling even though Other is now read locally.
			snapshotBranchTipAt(ctx, deps, target, "pull", ours, ours,
				tipOf(ctx, deps, target), target, remote+"/"+target)
			return Result{Changed: true}.WithSummary("fast-forwarded %s", target), nil
		}
		resp, derr := deps.decide(ctx, PromptReq("not-fast-forwardable", "Cannot fast-forward %s in the background", []string{"checkout-and-resolve", "abort"}, target))
		if derr != nil {
			return Result{}, derr
		}
		if resp.Option == "checkout-and-resolve" {
			// Held only a RefWrite reservation so far; checkoutPull touches
			// the worktree. This boundary is safe to escalate across: the
			// failed FastForwardRef left no partial state.
			if err := deps.escalate(ctx); err != nil {
				return Result{}, err
			}
			// FastForwardRef failed, so the branch never moved and nothing was
			// recorded above — checkoutPull does its own fetch + snapshot, as
			// on every other path into it.
			return op.checkoutPull(ctx, deps, remote, target, cur)
		}
		return Result{}.WithSummary("aborted: %s not fast-forwardable", target), nil
	}

	return op.checkoutPull(ctx, deps, remote, target, "")
}

// fetchThenSnapshotPull records branch's pre-pull version with a FRESH Other.
// The fetch has to come FIRST: Other is "<remote>/<branch>", and
// snapshotBranchTipNamed resolves it to a sha at record time, so a snapshot
// taken before the fetch freezes the tip of the PREVIOUS fetch and the
// after-side comparison then reports the commits this pull brought in as
// drift. A failed fetch skips the snapshot rather than recording stale
// endpoints — like everything else here, best-effort: it never fails the pull.
//
// Only checkoutPull uses this now. The background fast-forward records
// through snapshotBranchTipAt instead, after its single fetch, since there
// the fetch that lands the update is also the one that reveals Other.
func fetchThenSnapshotPull(ctx context.Context, deps OpDeps, remote, branch string) {
	deps.emit(ctx, Progress{Step: "fetching", Detail: remote})
	if err := deps.Repo.Fetch(ctx, remote); err != nil {
		return
	}
	snapshotBranchTip(ctx, deps, branch, "pull", tipOf(ctx, deps, branch), remote+"/"+branch)
}

func (op SmartPull) pullCurrent(ctx context.Context, deps OpDeps, remote, branch string) (Result, error) {
	deps.emit(ctx, Progress{Step: "fetching", Detail: remote})
	if err := deps.Repo.Fetch(ctx, remote); err != nil {
		if err = healStaleFetchMappings(ctx, deps, remote, err, func(ctx context.Context) error {
			return deps.Repo.Fetch(ctx, remote)
		}); err != nil {
			return Result{}, err
		}
	}
	// ONE snapshot, here: after the fetch (so Other is the tip this pull is
	// about to land on, not the previous fetch's) and before the first thing
	// that can move the branch. Every armed path below then has a record —
	// including the fast-forward, which used to leave DriftAfter comparing the
	// branch against some OLDER version and reporting the ff's own commits as
	// drift. A ff records Base == Ours (nothing was contributed on top of the
	// upstream tip), and DriftSince's empty-before-set skip makes that silent.
	snapshotBranchTip(ctx, deps, branch, "pull", tipOf(ctx, deps, branch), remote+"/"+branch)
	deps.emit(ctx, Progress{Step: "pulling (ff-only)", Detail: branch})
	if err := deps.Repo.Pull(ctx, remote, branch, git.PullFF); err == nil {
		return Result{Changed: true}.WithSummary("pulled %s", branch), nil
	}
	resp, derr := deps.decide(ctx, PromptReq("non-fast-forward", "%s has diverged from %s (reset discards local commits and changes)", []string{"rebase", "merge", "reset", "abort"}, branch, remote))
	if derr != nil {
		return Result{}, derr
	}
	switch resp.Option {
	case "rebase":
		if err := deps.Repo.Pull(ctx, remote, branch, git.PullRebase); err != nil {
			return Result{}, err
		}
		return Result{Changed: true}.WithSummary("pulled (rebased) %s", branch), nil
	case "merge":
		if err := deps.Repo.Pull(ctx, remote, branch, git.PullMerge); err != nil {
			return Result{}, err
		}
		return Result{Changed: true}.WithSummary("pulled (merged) %s", branch), nil
	case "reset":
		// The ff-only pull above failed WITHOUT starting a merge or rebase (that
		// is the --ff-only guarantee), so there is no in-progress state to abort:
		// reset --hard alone snaps the branch to the fetched remote tip and
		// discards local commits + uncommitted changes, as the user asked.
		remoteTip := remote + "/" + branch
		deps.emit(ctx, Progress{Step: "resetting (hard)", Detail: remoteTip})
		if err := deps.Repo.Reset(ctx, "hard", remoteTip); err != nil {
			return Result{}, err
		}
		return Result{Changed: true}.WithSummary("reset %s to %s (local changes discarded)", branch, remoteTip), nil
	default:
		return Result{}.WithSummary("aborted: %s diverged", branch), nil
	}
}

// checkoutPull pulls target by checking it out (or pulling inside the
// worktree that already has it). It always fetches and records target's
// pre-op version itself: every caller reaches here with the branch still
// unmoved (the background ff-ref path only falls through when its own fetch
// FAILED), so there is never a record to reuse.
func (op SmartPull) checkoutPull(ctx context.Context, deps OpDeps, remote, target, returnTo string) (Result, error) {
	repo := deps.Repo

	// Fetch + snapshot FIRST, above the worktree branch: both branches below
	// pull, so both need a recorded pre-op version, and the fetch has to
	// precede the record so Other is the tip being landed on (see
	// fetchThenSnapshotPull). Fetching before the stash/switch is safe —
	// a fetch touches no worktree — and the Pull below re-fetches anyway,
	// so this is not an extra network round-trip in the failure case.
	fetchThenSnapshotPull(ctx, deps, remote, target)

	wt, err := repo.WorktreeForBranch(ctx, target)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		deps.emit(ctx, Progress{Step: "pulling in worktree", Detail: wt.Path})
		if err := repo.PullInWorktree(ctx, wt.Path, remote, target); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: PromptReq("worktree-pull-failed", "Pull in worktree %s failed", []string{"abort"}, wt.Path)})
			return Result{}, fmt.Errorf("smart pull: worktree %s: %w", wt.Path, err)
		}
		return Result{Changed: true}.WithSummary("pulled %s in worktree %s", target, wt.Path), nil
	}

	dirty, err := repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := repo.StashPush(ctx, "gg-autostash:"+target, nil, false); err != nil {
			return Result{}, err
		}
		stashed = true
	}

	deps.emit(ctx, Progress{Step: "switching", Detail: target})
	if err := repo.Switch(ctx, target); err != nil {
		if stashed {
			_ = repo.StashPop(ctx, "")
		}
		return Result{}, err
	}

	deps.emit(ctx, Progress{Step: "pulling (ff-only)", Detail: target})
	pullErr := repo.Pull(ctx, remote, target, git.PullFF)
	res := Result{Changed: true}.WithSummary("pulled %s", target)

	if returnTo != "" && returnTo != target {
		deps.emit(ctx, Progress{Step: "switching back", Detail: returnTo})
		if err := repo.Switch(ctx, returnTo); err != nil {
			if stashed {
				_ = repo.StashPop(ctx, "") // restore on target rather than strand the stash
				stashed = false
			}
			return res.AppendSummary("; could not switch back to %s (changes restored on %s)", returnTo, target),
				fmt.Errorf("smart pull: switch back to %s failed: %w", returnTo, err)
		}
	}

	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := repo.StashPop(ctx, ""); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: PromptReq("stash-pop-conflict", "Restoring your changes conflicted", []string{"keep", "abort"})})
			return res.AppendSummary("; restore conflicted (changes preserved in stash)"),
				fmt.Errorf("stash pop conflict after pulling %s: %w", target, err)
		}
	}
	if pullErr != nil {
		return Result{}, fmt.Errorf("smart pull: %s: %w", target, pullErr)
	}
	return res, nil
}
