package domain

import (
	"context"
	"path/filepath"
	"strings"
)

// WorkspacesPath is the workspace registry's file beside the repositories
// registry (repos.toml → workspaces.toml in the same state dir); "" when
// recording is off ("" in).
func WorkspacesPath(reposStatePath string) string {
	if reposStatePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(reposStatePath), "workspaces.toml")
}

// RepoMainPath is the repository's identity for a workspace: its MAIN
// checkout path — the parent of the common git dir — whichever worktree
// this service is rooted in; for a bare repository (a common dir that is
// not a ".git" directory) the common dir itself. One git invocation.
func (s *Service) RepoMainPath(ctx context.Context) (string, error) {
	common, err := s.GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	common = filepath.Clean(common)
	if strings.EqualFold(filepath.Base(common), ".git") {
		return filepath.Dir(common), nil // a normal repository: the checkout beside its .git
	}
	// The common dir is not a ".git" directory: a bare repository (the
	// directory itself is the identity) or a submodule, whose git dir
	// lives under the superproject's .git/modules — git's own worktree
	// list names that git dir, so the identity is the checkout's top level.
	if wts, err := s.Worktrees(ctx); err == nil && len(wts) > 0 && wts[0].Bare {
		return filepath.Clean(wts[0].Path), nil
	}
	if top, err := s.TopLevel(ctx); err == nil && top != "" {
		return filepath.Clean(top), nil
	}
	return common, nil
}
