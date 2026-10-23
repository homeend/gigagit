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
		return filepath.Dir(common), nil
	}
	return common, nil
}
