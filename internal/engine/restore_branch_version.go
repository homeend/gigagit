package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
)

// RestoreBranchVersion moves Branch back to a recorded version ref. Current
// branch: a hard reset (dirty tree forks a proceed/cancel decision). A branch
// checked out in another worktree is refused. Any other branch — including a
// DELETED one — is moved (or recreated) via update-ref. The pre-restore tip
// is snapshotted first, so restore is itself undoable. Default TreeWrite lock
// (the current-branch lane touches the working tree).
type RestoreBranchVersion struct {
	Branch string // required
	Ref    string // required: a refs/gg/versions/... ref of Branch
}

var _ Operation = RestoreBranchVersion{}

func (op RestoreBranchVersion) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Branch == "" || op.Ref == "" {
		return Result{}, fmt.Errorf("restore version: Branch and Ref are required")
	}
	refBranch, _, _, ok := git.ParseVersionRef(op.Ref)
	if !ok || refBranch != op.Branch {
		return Result{}, fmt.Errorf("restore version: %s is not a version of branch %s", op.Ref, op.Branch)
	}
	sha, err := deps.Repo.RevParse(ctx, op.Ref)
	if err != nil {
		return Result{}, fmt.Errorf("restore version: %w", err)
	}
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}

	short := sha
	if len(short) > 8 {
		short = short[:8]
	}
	if op.Branch == cur {
		dirty, derr := deps.Repo.IsDirty(ctx)
		if derr != nil {
			return Result{}, derr
		}
		if dirty {
			resp, perr := deps.decide(ctx, PromptReq("restore-dirty",
				"the working tree has uncommitted changes; restoring %s discards them",
				[]string{"proceed", "cancel"}, op.Branch))
			if perr != nil {
				return Result{}, perr
			}
			if resp.Option != "proceed" {
				return Result{Changed: false}.WithSummary("cancelled"), nil
			}
		}
		snapshotBranchTip(ctx, deps, op.Branch, "restore")
		deps.emit(ctx, Progressf("restoring branch version", "%s → %s", op.Branch, short))
		if err := deps.Repo.Reset(ctx, "hard", sha); err != nil {
			return Result{}, fmt.Errorf("restore version: %w", err)
		}
	} else {
		wt, werr := deps.Repo.WorktreeForBranch(ctx, op.Branch)
		if werr != nil {
			return Result{}, werr
		}
		if wt != nil {
			return Result{}, fmt.Errorf("restore version: %s is checked out in worktree %s — restore it there", op.Branch, wt.Path)
		}
		snapshotBranchTip(ctx, deps, op.Branch, "restore")
		deps.emit(ctx, Progressf("restoring branch version", "%s → %s", op.Branch, short))
		if err := deps.Repo.UpdateRef(ctx, "refs/heads/"+op.Branch, sha); err != nil {
			return Result{}, fmt.Errorf("restore version: %w", err)
		}
	}

	res := Result{Changed: true}.WithSummary("restored %s to %s", op.Branch, short)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
