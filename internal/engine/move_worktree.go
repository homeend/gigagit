package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MoveWorktree relocates a linked worktree directory to Dest
// (`git worktree move`). Rename is the same op — callers compute Dest from
// the old parent + the new name. A locked worktree resolves reactively via
// the Decider; every other refusal (existing dest, submodules) surfaces as an
// error. Default TreeWrite reservation. Result.Path carries Dest so
// frontends can chain a switch when the current worktree moved.
type MoveWorktree struct {
	Path string // absolute path of the worktree to move
	Dest string // absolute destination path
}

func (op MoveWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Path == "" || op.Dest == "" {
		return Result{}, fmt.Errorf("move worktree: Path and Dest are required")
	}
	if samePath(op.Path, op.Dest) {
		return Result{Changed: false}.WithSummary("nothing to move: source and destination are the same"), nil
	}
	if rel, err := filepath.Rel(op.Path, op.Dest); err == nil &&
		rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("move worktree: destination %s is inside the worktree being moved", op.Dest)
	}
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return Result{}, err
	}
	// `git worktree list` always lists the main (primary) worktree first.
	if len(wts) == 0 {
		return Result{}, fmt.Errorf("move worktree: no worktrees listed")
	}
	if samePath(op.Path, wts[0].Path) {
		return Result{}, fmt.Errorf("move worktree: cannot move the main worktree (%s)", op.Path)
	}
	if _, err := os.Stat(op.Dest); err == nil {
		return Result{}, fmt.Errorf("move worktree: destination %s already exists", op.Dest)
	}
	if parent := filepath.Dir(op.Dest); parent != "" {
		if _, err := os.Stat(parent); err != nil {
			return Result{}, fmt.Errorf("move worktree: destination parent %s does not exist", parent)
		}
	}

	deps.emit(ctx, Progress{Step: "moving worktree", Detail: op.Dest})
	onLine := func(line string) { deps.emit(ctx, GitLine{Raw: line}) }
	// Run from the MAIN worktree so the git subprocess's cwd is never inside
	// the tree being moved (Windows cannot rename a process's cwd).
	if err := deps.Repo.MoveWorktree(ctx, wts[0].Path, op.Path, op.Dest, onLine); err != nil {
		if !isLockedWorktreeErr(err) {
			return Result{}, err
		}
		choice, derr := deps.decide(ctx, PromptReq("move-worktree-locked", "Worktree %s is locked. Unlock and move?", []string{"unlock-and-move", "abort"}, op.Path))
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != "unlock-and-move" {
			return Result{Changed: false}.WithSummary("cancelled; worktree not moved"), nil
		}
		if err := deps.Repo.UnlockWorktree(ctx, op.Path); err != nil {
			return Result{}, fmt.Errorf("unlock worktree: %w", err)
		}
		if err := deps.Repo.MoveWorktree(ctx, wts[0].Path, op.Path, op.Dest, onLine); err != nil {
			return Result{}, fmt.Errorf("move worktree (after unlock): %w", err)
		}
	}

	res := Result{Changed: true, Path: op.Dest}.WithSummary("moved worktree %s → %s", op.Path, op.Dest)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = MoveWorktree{}
