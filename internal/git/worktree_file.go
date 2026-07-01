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
