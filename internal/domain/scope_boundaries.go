package domain

import (
	"context"
	"sort"
	"strings"
)

// ScopeBoundaries returns the fork commits between a scoped (soloed) commit
// view and the repo's other branches: for every scope entry × other pair, the
// merge-base — the commit where the other branch's territory begins inside the
// scoped history. Ref decorations alone cannot mark that commit once the other
// branch has moved on past the fork (its tip then sits outside the scoped
// walk), which is the common case; merge-base finds it regardless.
//
// N sequential merge-base invocations under one Read reservation (ms each on a
// packed commit-graph; callers run this off-thread). Best-effort per pair: a
// bad ref or unrelated history is skipped, never an error. Criss-cross
// histories have several merge bases; git's single best one is accepted —
// territories are view decoration, not merge input. Results are deduped and
// sorted full shas (full so hash-set matching against %H can never miss).
func (s *Service) ScopeBoundaries(ctx context.Context, scope, others []string) ([]string, error) {
	key := "scope-boundaries:" + strings.Join(scope, ",") + "|" + strings.Join(others, ",")
	return query(ctx, s, key, func(ctx context.Context) ([]string, error) {
		seen := map[string]bool{}
		var out []string
		for _, sc := range scope {
			for _, o := range others {
				if o == sc {
					continue
				}
				base, err := s.repo.MergeBase(ctx, sc, o)
				if err != nil || base == "" || seen[base] {
					continue // no common ancestor / vanished ref: skip, best-effort
				}
				seen[base] = true
				out = append(out, base)
			}
		}
		sort.Strings(out)
		return out, nil
	})
}
