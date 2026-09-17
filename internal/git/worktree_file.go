package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// worktreePath resolves path (repo-root-relative, slash-separated) under top,
// rejecting a result that escapes the working tree. filepath.Join Cleans the
// joined path, which collapses ".." segments upward instead of rejecting them,
// so containment is checked explicitly — the destination may be raw user input
// (a CLI positional argument or a TUI text field) funneled through WriteFile.
func worktreePath(top, path string) (string, error) {
	full := filepath.Join(top, filepath.FromSlash(path))
	rel, err := filepath.Rel(top, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the working tree", path)
	}
	return full, nil
}

// ReadWorktreeFile reads path (repo-root-relative, slash-separated) from the
// working tree. Used to load a conflicted file's marker text for the hunk
// picker. Not a git invocation — a plain filesystem read, located here because
// the repo already resolves its own top-level.
func (r *Repo) ReadWorktreeFile(ctx context.Context, path string) ([]byte, error) {
	top, err := r.TopLevel(ctx)
	if err != nil {
		return nil, err
	}
	full, err := worktreePath(top, path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

// WorktreeFilesPresent reports, for each given path (repo-root-relative,
// slash-separated), whether the working tree actually HAS that file on disk.
//
// Deliberately a stat, not a git invocation: git has no listing that answers
// "what is on disk". `ls-files` reads the INDEX, and `ls-files --deleted` —
// the obvious candidate — does not see a SKIP-WORKTREE entry at all, so on a
// sparse checkout it calls every sparse-excluded path present. Those are the
// deployments this repo targets, so the filesystem is made the authority on
// the filesystem. It also gets untracked-but-present right for free.
//
// os.Lstat, not os.Stat: a dangling symlink IS an entry in the working tree
// (git tracks the link, not its target).
//
// A path that escapes the working tree is reported ABSENT rather than
// erroring: it cannot be a member of the tree, and one malformed entry in a
// bounded side must not fail the whole comparison. The result holds an entry
// for every input path.
func (r *Repo) WorktreeFilesPresent(ctx context.Context, paths []string) (map[string]bool, error) {
	out := make(map[string]bool, len(paths))
	if len(paths) == 0 {
		return out, nil
	}
	top, err := r.TopLevel(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		full, err := worktreePath(top, p)
		if err != nil {
			out[p] = false
			continue
		}
		_, statErr := os.Lstat(full)
		out[p] = statErr == nil
	}
	return out, nil
}

// WriteWorktreeFile writes content to path (repo-root-relative) in the working
// tree, truncating an existing file (its mode is preserved by the OS since the
// file already exists). Missing parent directories are created, so it can also
// drop a shelf restore at a brand-new path. Used by ResolveConflictHunks to
// write the assembled resolution before staging, and by WriteFile. A path that
// escapes the working tree is rejected.
func (r *Repo) WriteWorktreeFile(ctx context.Context, path string, content []byte) error {
	top, err := r.TopLevel(ctx)
	if err != nil {
		return err
	}
	full, err := worktreePath(top, path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, content, 0o644)
}
