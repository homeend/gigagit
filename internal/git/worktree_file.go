package git

import (
	"context"
	"os"
	"path/filepath"
)

// ReadWorktreeFile reads path (repo-root-relative, slash-separated) from the
// working tree. Used to load a conflicted file's marker text for the hunk
// picker. Not a git invocation — a plain filesystem read, located here because
// the repo already resolves its own top-level.
func (r *Repo) ReadWorktreeFile(ctx context.Context, path string) ([]byte, error) {
	top, err := r.TopLevel(ctx)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(top, filepath.FromSlash(path)))
}

// WriteWorktreeFile writes content to path (repo-root-relative) in the working
// tree, truncating an existing file (its mode is preserved by the OS since the
// file already exists). Used by ResolveConflictHunks to write the assembled
// resolution before staging.
func (r *Repo) WriteWorktreeFile(ctx context.Context, path string, content []byte) error {
	top, err := r.TopLevel(ctx)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(top, filepath.FromSlash(path)), content, 0o644)
}
