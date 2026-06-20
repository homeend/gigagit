package engine

import (
	"context"
	"errors"
)

// ConflictAction is a whole-file resolution choice.
type ConflictAction int

const (
	KeepOurs ConflictAction = iota
	KeepTheirs
	MarkResolved
	DeleteFile
	KeepBase
)

// ResolveConflict resolves one conflicted file at the whole-file level, then
// stages it (or removes it) so the unmerged index entry clears.
type ResolveConflict struct {
	Path   string
	Action ConflictAction
}

func (op ResolveConflict) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "resolving", Detail: op.Path})
	var summary string
	switch op.Action {
	case KeepOurs:
		if err := deps.Repo.CheckoutSide(ctx, op.Path, "ours"); err != nil {
			return Result{}, err
		}
		if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
			return Result{}, err
		}
		summary = "resolved " + op.Path + " (kept ours)"
	case KeepTheirs:
		if err := deps.Repo.CheckoutSide(ctx, op.Path, "theirs"); err != nil {
			return Result{}, err
		}
		if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
			return Result{}, err
		}
		summary = "resolved " + op.Path + " (kept theirs)"
	case KeepBase:
		if err := deps.Repo.CheckoutBaseStage(ctx, op.Path); err != nil {
			return Result{}, err
		}
		if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
			return Result{}, err
		}
		summary = "resolved " + op.Path + " (kept base)"
	case DeleteFile:
		if err := deps.Repo.RemoveFile(ctx, op.Path); err != nil {
			return Result{}, err
		}
		summary = "resolved " + op.Path + " (deleted)"
	case MarkResolved:
		if err := deps.Repo.StagePaths(ctx, []string{op.Path}); err != nil {
			return Result{}, err
		}
		summary = "marked " + op.Path + " resolved"
	default:
		return Result{}, errors.New("unknown conflict action")
	}
	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// MarkAllResolved stages every given path (the user edited them by hand).
type MarkAllResolved struct{ Paths []string }

func (op MarkAllResolved) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "resolving", Detail: "all"})
	if err := deps.Repo.StagePaths(ctx, op.Paths); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "marked all resolved", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// ContinueOp finalizes whichever of merge/rebase/cherry-pick is in progress.
// Cherry-pick is probed LAST: a paused rebase pick also sets CHERRY_PICK_HEAD,
// so rebase must win the dispatch.
type ContinueOp struct{}

func (op ContinueOp) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "continuing", Detail: ""})
	if ok, _ := deps.Repo.MergeInProgress(ctx, ""); ok {
		if err := deps.Repo.MergeContinue(ctx, ""); err != nil {
			return Result{}, err
		}
		return conflictDone(deps, ctx, "merge continued")
	}
	if ok, _ := deps.Repo.RebaseInProgress(ctx, ""); ok {
		if err := deps.Repo.RebaseContinue(ctx, ""); err != nil {
			return Result{}, err
		}
		return conflictDone(deps, ctx, "rebase continued")
	}
	if ok, _ := deps.Repo.CherryPickInProgress(ctx, ""); ok {
		if err := deps.Repo.CherryPickContinue(ctx, ""); err != nil {
			return Result{}, err
		}
		return conflictDone(deps, ctx, "cherry-pick continued")
	}
	return Result{}, errors.New("no merge, rebase, or cherry-pick in progress")
}

// AbortOp aborts whichever of merge/rebase/cherry-pick is in progress.
// Cherry-pick is probed LAST (see ContinueOp).
type AbortOp struct{}

func (op AbortOp) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "aborting", Detail: ""})
	if ok, _ := deps.Repo.MergeInProgress(ctx, ""); ok {
		if err := deps.Repo.MergeAbort(ctx, ""); err != nil {
			return Result{}, err
		}
		return conflictDone(deps, ctx, "merge aborted")
	}
	if ok, _ := deps.Repo.RebaseInProgress(ctx, ""); ok {
		if err := deps.Repo.RebaseAbort(ctx, ""); err != nil {
			return Result{}, err
		}
		return conflictDone(deps, ctx, "rebase aborted")
	}
	if ok, _ := deps.Repo.CherryPickInProgress(ctx, ""); ok {
		if err := deps.Repo.CherryPickAbort(ctx, ""); err != nil {
			return Result{}, err
		}
		return conflictDone(deps, ctx, "cherry-pick aborted")
	}
	return Result{}, errors.New("no merge, rebase, or cherry-pick in progress")
}

func conflictDone(deps OpDeps, ctx context.Context, summary string) (Result, error) {
	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var (
	_ Operation = ResolveConflict{}
	_ Operation = MarkAllResolved{}
	_ Operation = ContinueOp{}
	_ Operation = AbortOp{}
)
