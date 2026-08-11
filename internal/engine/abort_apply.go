package engine

import (
	"context"
)

// AbortApply discards a conflicted APPLICATION: a stash apply (or a
// checkout/merge-file lane) that left unmerged paths WITHOUT a paused
// sequencer op — the state AbortOp cannot help with (nothing is paused) and
// Discard refuses (conflicted paths). `git reset --merge HEAD`: conflict
// markers are removed and the conflicted files return to HEAD, while
// unrelated local changes — tracked edits and untracked files — and the
// stash entry itself are kept (measured), so the apply can be retried.
// Decision-free; the frontend owns both the confirm and the two refusals
// (nothing conflicted / a paused op owns the conflicts).
type AbortApply struct{}

var _ Operation = AbortApply{}

func (op AbortApply) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "discarding conflicted apply"})
	if err := deps.Repo.Reset(ctx, "merge", "HEAD"); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("discarded the conflicted apply — conflicted files reset to HEAD; other changes and the stash kept")
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
