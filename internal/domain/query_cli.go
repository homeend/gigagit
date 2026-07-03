package domain

// Queries backing the agent-facing CLI read verbs (gg log / diff / show /
// branch current). Same contract as query.go: Read reservation, singleflight
// per key, NoteFailure on genuine errors.

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// Log returns up to n terse history rows from rev (branch, sha, or A..B
// range), newest first.
func (s *Service) Log(ctx context.Context, rev string, n int) ([]model.LogLine, error) {
	return query(ctx, s, "log:"+rev+":"+strconv.Itoa(n), func(ctx context.Context) ([]model.LogLine, error) {
		return s.repo.LogLines(ctx, rev, n)
	})
}

// CommitLine returns rev's short sha and subject.
func (s *Service) CommitLine(ctx context.Context, rev string) (model.LogLine, error) {
	return query(ctx, s, "commitline:"+rev, func(ctx context.Context) (model.LogLine, error) {
		return s.repo.CommitLine(ctx, rev)
	})
}

// DiffStat returns terse per-file change stats for spec.
func (s *Service) DiffStat(ctx context.Context, spec model.DiffSpec) ([]model.DiffStat, error) {
	key := "diffstat:" + strconv.FormatBool(spec.Cached) + ":" + spec.Rev + ":" + strings.Join(spec.Paths, "\x00")
	return query(ctx, s, key, func(ctx context.Context) ([]model.DiffStat, error) {
		out, err := s.repo.DiffNumstat(ctx, spec)
		if err != nil {
			return nil, err
		}
		return git.ParseNumstat(out), nil
	})
}

// DiffPatch returns the full patch text for spec.
func (s *Service) DiffPatch(ctx context.Context, spec model.DiffSpec) (string, error) {
	key := "diffpatch:" + strconv.FormatBool(spec.Cached) + ":" + spec.Rev + ":" + strings.Join(spec.Paths, "\x00")
	return query(ctx, s, key, func(ctx context.Context) (string, error) {
		return s.repo.DiffPatch(ctx, spec)
	})
}
