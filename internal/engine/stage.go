package engine

import (
	"context"
	"fmt"
	"strings"
)

// Stage stages (or, with Unstage, unstages) the given paths in the index.
// All stages everything including untracked files (git add -A) and is
// mutually exclusive with Paths and Unstage. It takes no decisions and
// emits a single Progress; the default TreeWrite reservation applies (it
// writes .git/index).
type Stage struct {
	Paths   []string
	All     bool
	Unstage bool
}

var _ Operation = Stage{}

func (op Stage) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.All {
		if op.Unstage {
			return Result{}, fmt.Errorf("stage: All cannot unstage")
		}
		if len(op.Paths) > 0 {
			return Result{}, fmt.Errorf("stage: All and explicit paths are mutually exclusive")
		}
		deps.emit(ctx, Progress{Step: "staged", Detail: "all changes"})
		if err := deps.Repo.StageAll(ctx); err != nil {
			return Result{}, fmt.Errorf("stage: %w", err)
		}
		return Result{Changed: true}.WithSummary("staged all changes"), nil
	}
	if len(op.Paths) == 0 {
		return Result{}, fmt.Errorf("stage: no paths")
	}
	verb := "staged"
	if op.Unstage {
		verb = "unstaged"
	}
	deps.emit(ctx, Progress{Step: verb, Detail: strings.Join(op.Paths, " ")})
	var err error
	if op.Unstage {
		err = deps.Repo.UnstagePaths(ctx, op.Paths)
	} else {
		err = deps.Repo.StagePaths(ctx, op.Paths)
	}
	if err != nil {
		return Result{}, fmt.Errorf("stage: %w", err)
	}
	if op.Unstage {
		return Result{Changed: true}.WithSummary("unstaged %s", strings.Join(op.Paths, " ")), nil
	}
	return Result{Changed: true}.WithSummary("staged %s", strings.Join(op.Paths, " ")), nil
}
