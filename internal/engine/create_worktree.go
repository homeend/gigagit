package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CreateWorktree creates a new linked worktree on a NEW branch (Branch) based on
// StartPoint, at Path. A relative Path is resolved against the repository root.
// The fields are fully resolved by the frontend (template resolution and any
// <user:> input happen there, not here — see spec §3).
type CreateWorktree struct {
	StartPoint     string
	Branch         string
	Path           string
	PostCreateHook string // shell script run in the new worktree; "" = none
}

func (op CreateWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.StartPoint == "" || op.Branch == "" || op.Path == "" {
		return Result{}, fmt.Errorf("create worktree: StartPoint, Branch, and Path are required")
	}

	// Validate the branch name before the (potentially large) checkout so an
	// illegal template result fails fast with a clear message.
	if err := deps.Repo.CheckRefFormatBranch(ctx, op.Branch); err != nil {
		return Result{}, fmt.Errorf("create worktree: invalid branch name %q: %w", op.Branch, err)
	}

	abs, err := resolveNewWorktreePath(ctx, deps, op.Path)
	if err != nil {
		return Result{}, err
	}

	deps.emit(ctx, Progress{Step: "creating worktree", Detail: op.Branch + " → " + abs})
	if err := deps.Repo.AddWorktree(ctx, abs, op.Branch, op.StartPoint, func(line string) {
		deps.emit(ctx, GitLine{Raw: line})
	}); err != nil {
		return Result{}, fmt.Errorf("create worktree: %w", err)
	}

	res := runPostCreateHook(ctx, deps, Result{Changed: true, Path: abs}.WithSummary("worktree created: %s", abs), abs, op.Branch, op.PostCreateHook)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// resolveNewWorktreePath resolves a possibly-relative worktree path against
// the MAIN worktree root and refuses a path that already exists. The runner's
// working directory may be a subdirectory — or a linked worktree — of the repo,
// so a relative path must not be left for git to interpret against its own cwd,
// and must not anchor on the current (linked) worktree: otherwise the new
// worktree nests under it (the default "../<repo>.worktrees/<branch>" template
// then doubles the ".worktrees" segment). The main worktree is git's stable
// anchor regardless of where gg is invoked.
func resolveNewWorktreePath(ctx context.Context, deps OpDeps, path string) (string, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		base, err := mainWorktreeRoot(ctx, deps)
		if err != nil {
			return "", err
		}
		abs = filepath.Clean(filepath.Join(base, path))
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("create worktree: path already exists: %s", abs)
	}
	return abs, nil
}

// mainWorktreeRoot returns the working-tree root of the repository's main
// worktree — git's `worktree list` always reports it first, regardless of which
// (linked) worktree gg was invoked from. Falls back to the current worktree's
// top-level if the list is empty or its first entry has no path.
func mainWorktreeRoot(ctx context.Context, deps OpDeps) (string, error) {
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	if len(wts) > 0 && wts[0].Path != "" {
		return wts[0].Path, nil
	}
	return deps.Repo.TopLevel(ctx)
}

// compile-time check that CreateWorktree satisfies Operation.
var _ Operation = CreateWorktree{}
