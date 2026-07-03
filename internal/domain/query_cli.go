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

// ShowStat returns rev's header line plus terse per-file stats.
func (s *Service) ShowStat(ctx context.Context, rev string, paths []string) (model.LogLine, []model.DiffStat, error) {
	type showStat struct {
		line  model.LogLine
		stats []model.DiffStat
	}
	v, err := query(ctx, s, "showstat:"+rev+":"+strings.Join(paths, "\x00"), func(ctx context.Context) (showStat, error) {
		line, err := s.repo.CommitLine(ctx, rev)
		if err != nil {
			return showStat{}, err
		}
		out, err := s.repo.ShowNumstat(ctx, rev, paths)
		if err != nil {
			return showStat{}, err
		}
		return showStat{line: line, stats: git.ParseNumstat(out)}, nil
	})
	return v.line, v.stats, err
}

// ShowPatch returns rev's header line plus its full patch text.
func (s *Service) ShowPatch(ctx context.Context, rev string, paths []string) (model.LogLine, string, error) {
	type showPatch struct {
		line  model.LogLine
		patch string
	}
	v, err := query(ctx, s, "showpatch:"+rev+":"+strings.Join(paths, "\x00"), func(ctx context.Context) (showPatch, error) {
		line, err := s.repo.CommitLine(ctx, rev)
		if err != nil {
			return showPatch{}, err
		}
		patch, err := s.repo.ShowPatch(ctx, rev, paths)
		if err != nil {
			return showPatch{}, err
		}
		return showPatch{line: line, patch: patch}, nil
	})
	return v.line, v.patch, err
}
