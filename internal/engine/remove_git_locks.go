package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/git"
)

// RemoveGitLocks deletes stranded git lockfiles (`.git/index.lock` and
// friends) so the repository becomes usable again.
//
// A lockfile only outlives its git process when that process died without
// running its cleanup handler — a hard kill, a power loss, a crash. gg itself
// used to be the usual cause: cancelling a git subprocess SIGKILLed it, which
// git cannot trap. That is fixed at the source (gitexec terminates gracefully
// so git removes its own locks), but the graceful path does not exist on
// Windows and cannot help a lock left by some other tool, so this op is the
// recovery path.
//
// Deleting a lock that a LIVE git still holds would corrupt that git's write,
// so this op is deliberately conservative:
//
//   - It takes the default TreeWrite reservation — fully exclusive — so no gg
//     operation can be running git concurrently.
//   - It refuses any path that is not a known lockfile name (git.IsLockFilePath)
//     inside this repository's git dirs, so a frontend bug cannot turn it into
//     an arbitrary file delete (the DeleteBranchVersion guard precedent).
//   - It does NOT decide staleness. gg cannot see git processes started
//     outside it, so the judgement stays with the human: the frontend shows
//     each lock's age and asks. This op only carries out the decision.
//
// Writes via os directly rather than a git verb — the ExportToDir/ExportFile
// precedent for touching files outside the working tree.
type RemoveGitLocks struct {
	Paths []string // absolute paths to lockfiles; empty is a no-op
}

var _ Operation = RemoveGitLocks{}

func (op RemoveGitLocks) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if len(op.Paths) == 0 {
		return Result{Changed: false}.WithSummary("no git locks to remove"), nil
	}
	allowed, err := lockDirs(ctx, deps)
	if err != nil {
		return Result{}, fmt.Errorf("remove git locks: %w", err)
	}
	for _, p := range op.Paths {
		if err := checkLockPath(p, allowed); err != nil {
			return Result{}, fmt.Errorf("remove git locks: %w", err)
		}
	}

	removed := 0
	for _, p := range op.Paths {
		deps.emit(ctx, Progressf("removing git lock", "removing %s", filepath.Base(p)))
		err := os.Remove(p)
		if os.IsNotExist(err) {
			// Whoever held it finished and cleaned up between the scan and
			// now: the desired state, not a failure.
			continue
		}
		if err != nil {
			return Result{}, fmt.Errorf("remove git locks: %w", err)
		}
		removed++
	}
	if removed == 0 {
		return Result{Changed: false}.WithSummary("git locks were already gone"), nil
	}
	res := Result{Changed: true}
	if removed == 1 {
		res = res.WithSummary("removed 1 stale git lock")
	} else {
		res = res.WithSummary("removed %d stale git locks", removed)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// lockDirs returns the directories a lockfile may legally live in: this
// worktree's git dir and the repository's common dir (equal in the main
// worktree, different in a linked one).
func lockDirs(ctx context.Context, deps OpDeps) ([]string, error) {
	gd, err := deps.Repo.GitDir(ctx)
	if err != nil {
		return nil, err
	}
	cd, err := deps.Repo.GitCommonDir(ctx)
	if err != nil {
		return nil, err
	}
	return []string{strings.TrimSpace(gd), strings.TrimSpace(cd)}, nil
}

// checkLockPath enforces both halves of the guard: a known lockfile NAME, and
// a location directly inside one of the repository's git dirs. Paths are
// compared after EvalSymlinks-free cleaning on both sides; a relative path is
// refused outright rather than resolved against an ambient cwd, since the op
// may run from any worktree.
func checkLockPath(p string, allowed []string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%s is not an absolute path", p)
	}
	if !git.IsLockFilePath(p) {
		return fmt.Errorf("%s is not a git lockfile", p)
	}
	dir := filepath.Clean(filepath.Dir(p))
	for _, a := range allowed {
		if a != "" && filepath.Clean(a) == dir {
			return nil
		}
	}
	return fmt.Errorf("%s is outside this repository's git directory", p)
}
