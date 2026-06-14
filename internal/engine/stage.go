package engine

import (
	"context"
	"fmt"
	"strings"
)

// Stage stages (or, with Unstage, unstages) the given paths in the index. It
// takes no decisions and emits a single Progress; the default TreeWrite
// reservation applies (it writes .git/index).
type Stage struct {
	Paths   []string
	Unstage bool
}

var _ Operation = Stage{}

func (op Stage) Run(ctx context.Context, deps OpDeps) (Result, error) {
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
	return Result{Summary: verb + " " + strings.Join(op.Paths, " "), Changed: true}, nil
}
