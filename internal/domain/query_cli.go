package domain

// Queries backing the agent-facing CLI read verbs (gg log / diff / show /
// branch current). Same contract as query.go: Read reservation, singleflight
// per key, NoteFailure on genuine errors.

import (
	"context"
	"strconv"

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
