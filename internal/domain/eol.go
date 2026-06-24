package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/model"
)

// statusFiltered is repo.Status with EOL-only unstaged modifications reconciled
// away (unless the frontend opted to show them via SetShowEOLOnlyChanges). It
// backs BOTH the CLI Status query and the TUI startup Snapshot so the Files
// panel, the count badge, and `gg status` always agree.
func (s *Service) statusFiltered(ctx context.Context) (model.WorkingTreeStatus, error) {
	st, err := s.repo.Status(ctx)
	if err != nil {
		return st, err
	}
	return s.dropEOLOnly(ctx, st), nil
}

// dropEOLOnly reclassifies tracked files whose ONLY unstaged change is line
// endings (CRLF↔LF). `git status` flags them 'M' (the blob hash differs), but
// `git diff --ignore-cr-at-eol` (via ModifiedIgnoringEOL) reports no real
// change. For each such file the unstaged 'M' is cleared; the row is dropped
// only when it has no staged change left, so a staged-M + unstaged-EOL file
// keeps its staged entry. Best-effort: any error leaves status untouched, so a
// failure never hides a genuine change.
func (s *Service) dropEOLOnly(ctx context.Context, st model.WorkingTreeStatus) model.WorkingTreeStatus {
	if s.showEOLOnly.Load() {
		return st
	}
	var cands []string
	for _, f := range st.Files {
		if isUnstagedModified(f) {
			cands = append(cands, f.Path)
		}
	}
	if len(cands) == 0 {
		return st
	}
	real, err := s.repo.ModifiedIgnoringEOL(ctx, cands)
	if err != nil {
		return st
	}
	realSet := make(map[string]struct{}, len(real))
	for _, p := range real {
		realSet[p] = struct{}{}
	}

	out := make([]model.FileStatus, 0, len(st.Files))
	for _, f := range st.Files {
		if isUnstagedModified(f) {
			if _, stillModified := realSet[f.Path]; !stillModified {
				// EOL-only: drop the unstaged modification.
				f.Unstaged = '.'
				if f.Staged == '.' || f.Staged == 0 {
					continue // nothing left to show for this file
				}
			}
		}
		out = append(out, f)
	}
	st.Files = out
	return st
}

// isUnstagedModified reports a tracked file with an unstaged content
// modification ('M') — the only state line endings can produce. Deletes,
// type-changes and renames are never EOL artifacts and are left alone.
func isUnstagedModified(f model.FileStatus) bool {
	return f.Kind == model.KindTracked && f.Unstaged == 'M'
}
