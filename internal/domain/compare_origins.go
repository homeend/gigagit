package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNoMergeBase is returned by CompareOrigins when the two revisions share
// no common ancestor (unrelated histories), so per-branch origin sets are
// undefined. Callers detect it with errors.Is.
var ErrNoMergeBase = errors.New("no common ancestor")

// CompareOrigins attributes changed paths to each side of a branch
// comparison: for M = merge-base(a, b), APaths = paths touched by M..a and
// BPaths = paths touched by M..b (renames contribute both old and new path).
// Three git invocations under one Read reservation. Any merge-base failure
// maps to ErrNoMergeBase (wrapping the cause): callers pass refs taken from
// the branches list, so "bad ref" is not a distinct case worth surfacing.
func (s *Service) CompareOrigins(ctx context.Context, a, b string) (model.CompareOrigins, error) {
	return query(ctx, s, "compare-origins:"+a+":"+b, func(ctx context.Context) (model.CompareOrigins, error) {
		base, err := s.repo.MergeBase(ctx, a, b)
		if err != nil {
			return model.CompareOrigins{}, fmt.Errorf("%w: %v", ErrNoMergeBase, err)
		}
		aPaths, err := s.originPaths(ctx, base, a)
		if err != nil {
			return model.CompareOrigins{}, err
		}
		bPaths, err := s.originPaths(ctx, base, b)
		if err != nil {
			return model.CompareOrigins{}, err
		}
		return model.CompareOrigins{APaths: aPaths, BPaths: bPaths}, nil
	})
}

// originPaths returns the set of paths touched by base..tip, both rename
// sides included.
func (s *Service) originPaths(ctx context.Context, base, tip string) (map[string]bool, error) {
	out, err := s.repo.DiffNumstat(ctx, model.DiffSpec{Rev: base + ".." + tip})
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, st := range git.ParseNumstat(out) {
		set[st.Path] = true
		if st.OldPath != "" {
			set[st.OldPath] = true
		}
	}
	return set, nil
}
