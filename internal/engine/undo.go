package engine

import (
	"context"
	"fmt"
	"strings"
)

// UndoLastCommit reverts the most recent commit by moving the branch ref back
// one step (git reset --soft HEAD@{1}). The commit's changes are kept staged,
// never discarded. It refuses if the last HEAD movement was not a commit, so it
// never corrupts state by reversing an unrelated operation.
type UndoLastCommit struct{}

var _ Operation = UndoLastCommit{}

func (UndoLastCommit) Run(ctx context.Context, deps OpDeps) (Result, error) {
	subj, err := deps.Repo.LastReflogSubject(ctx)
	if err != nil {
		return Result{}, err
	}
	if !strings.HasPrefix(subj, "commit") {
		return Result{}, fmt.Errorf("undo: last operation was not a commit (%q); ref-only undo can only undo commits", subj)
	}
	if cur, cerr := deps.Repo.CurrentBranch(ctx); cerr == nil {
		snapshotBranchTip(ctx, deps, cur, "undo-commit")
	}
	deps.emit(ctx, Progress{Step: "undoing last commit", Detail: subj})
	if err := deps.Repo.ResetSoft(ctx, "HEAD@{1}"); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("undid last commit (changes kept staged)")
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
