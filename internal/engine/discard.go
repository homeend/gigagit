package engine

import (
	"context"
	"errors"
	"fmt"
)

// Discard throws away unstaged working-tree changes. Restore holds tracked
// paths to reset from the index (git restore --worktree, keeping staged hunks);
// Remove holds untracked paths to delete (git clean). When All is set, both
// lists are ignored and the whole working tree is discarded via a repo-root
// pathspec, avoiding argv blowup on large monorepos. Default TreeWrite
// reservation (it touches the working tree).
type Discard struct {
	Restore []string
	Remove  []string
	All     bool
}

var _ Operation = Discard{}

func (op Discard) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "discarding"})

	restore, remove := op.Restore, op.Remove
	if op.All {
		// ":/" anchors BOTH verbs to the repo root regardless of the runner's
		// cwd. git clean's default traversal is cwd-relative, so without this an
		// "all" discard launched from a subdirectory would revert tracked edits
		// repo-wide but only delete untracked files under that subdirectory.
		restore = []string{":/"}
		remove = []string{":/"}
	}

	// Run both steps even if the first errors, so we never leave a silent
	// half-discard; collect and join whatever failed.
	var errs []error
	if len(restore) > 0 {
		if err := deps.Repo.RestoreWorktree(ctx, restore); err != nil {
			errs = append(errs, fmt.Errorf("restore: %w", err))
		}
	}
	if len(remove) > 0 {
		if err := deps.Repo.CleanUntracked(ctx, remove); err != nil {
			errs = append(errs, fmt.Errorf("clean: %w", err))
		}
	}
	if len(errs) > 0 {
		return Result{}, errors.Join(errs...)
	}

	res := Result{Changed: true}.WithSummary("discarded")
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
