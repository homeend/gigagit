package domain

import (
	"context"
	"os"
	"path/filepath"
)

// commitGraphExists reports whether a commit-graph cache lives under the given
// git common dir — a single file or a split chain. Pure filesystem check (no git
// call, no reservation), so the Snapshot can reuse it with the common dir it
// already fetched.
func commitGraphExists(commonDir string) bool {
	if _, err := os.Stat(filepath.Join(commonDir, "objects", "info", "commit-graph")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(commonDir, "objects", "info", "commit-graphs", "commit-graph-chain")); err == nil {
		return true
	}
	return false
}

// HasCommitGraph reports whether the repo has a commit-graph cache. When present,
// `git log --date-order` is cheap.
func (s *Service) HasCommitGraph(ctx context.Context) (bool, error) {
	dir, err := s.GitCommonDir(ctx)
	if err != nil {
		return false, err
	}
	return commitGraphExists(dir), nil
}

// WriteCommitGraph writes/refreshes the commit-graph cache. It runs WITHOUT a gg
// reservation: `git commit-graph write` takes its own lockfile and touches no
// refs/tree/index, so holding gg's writer-preferring Read gate for the (~26 s on
// a huge repo) write would needlessly delay a queued user commit/switch.
func (s *Service) WriteCommitGraph(ctx context.Context) error {
	return s.repo.WriteCommitGraph(ctx)
}
