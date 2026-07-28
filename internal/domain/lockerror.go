package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// StaleLocks reports the git lockfiles present in this worktree's git dir and
// the repository's common dir. Stat-level only — no git invocation — so it is
// safe on the repo-load path (RepoHealth embeds the same call).
//
// Presence is not proof of staleness: a git running right now legitimately
// holds one. Callers show the age and let a human decide.
func (s *Service) StaleLocks(ctx context.Context) ([]model.GitLock, error) {
	return query(ctx, s, "stalelocks", func(ctx context.Context) ([]model.GitLock, error) {
		cd, err := s.repo.GitCommonDir(ctx)
		if err != nil {
			return nil, err
		}
		return git.LockFiles(s.gitDirCached(ctx), strings.TrimSpace(cd)), nil
	})
}

// IsLockError reports whether err is git refusing to run because a lockfile
// is already present ("Another git process seems to be running in this
// repository"). Frontends use it to offer the stale-lock recovery instead of
// leaving the user with an error they can only fix with a manual `rm`.
//
// This lives in domain because internal/tui and internal/cli may not import
// internal/git (archtest-guarded) — it is the same re-export role Conflict
// plays for the conflict probes.
func IsLockError(err error) bool { return git.IsLockError(err) }
