package domain

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/model"
)

// parseStashList splits each "stash@{N}: <subject>" line into a StashEntry.
// Lines without the "<ref>: " shape (e.g. blanks) are skipped.
func parseStashList(lines []string) []model.StashEntry {
	var out []model.StashEntry
	for _, ln := range lines {
		ref, subject, ok := strings.Cut(ln, ": ")
		if !ok || !strings.HasPrefix(strings.TrimSpace(ref), "stash@{") {
			continue
		}
		out = append(out, model.StashEntry{Ref: strings.TrimSpace(ref), Subject: subject})
	}
	return out
}

// StashList returns parsed stash entries (newest first) under a Read reservation.
func (s *Service) StashList(ctx context.Context) ([]model.StashEntry, error) {
	return query(ctx, s, "stash-list", func(ctx context.Context) ([]model.StashEntry, error) {
		lines, err := s.repo.StashList(ctx)
		if err != nil {
			return nil, err
		}
		return parseStashList(lines), nil
	})
}

// StashCommit resolves a stash ref to its commit SHA, under a Read reservation.
func (s *Service) StashCommit(ctx context.Context, ref string) (string, error) {
	return query(ctx, s, "stash-commit:"+ref, func(ctx context.Context) (string, error) {
		return s.repo.StashCommit(ctx, ref)
	})
}
