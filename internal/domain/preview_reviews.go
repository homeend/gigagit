package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/git"
)

// PreviewReviews are the reviews written for set (spec R2), newest first:
// current — its tip is the set's tip; older (Older) — the tip moved on but
// both commits the review read still exist, so the review view can open
// exactly what was reviewed; gone — a reviewed commit is no longer in the
// repository: omitted (View all notes still lists it). Matching is by the
// scope NAME, so a removed and re-added saved row, or a preview opened only
// from a link, finds the same reviews. A tip still in the set's commit list
// costs no git call; only a rewritten one is looked up. A pull request's
// reviews are the ones saved for it by number (prReviewHeads), classified
// the same way (spec §3.1).
func (s *Service) PreviewReviews(ctx context.Context, set PreviewNoteSet) ([]ReviewHead, error) {
	if _, pr := git.ParsePRRef(set.Source); pr {
		return s.classifyHeads(ctx, s.prReviewHeads(ctx, set), set.Tip, set.commitSet()), nil
	}
	sc := set.scope()
	if sc == "" {
		return nil, nil
	}
	return s.classifyScopeReviews(ctx, sc, set.Tip, set.commitSet())
}

// PreviewReviewsByScope is PreviewReviews for a scope that cannot be built
// today — a merged preview, a deleted source (spec §8): with no current tip,
// every review whose two commits still exist is an older one (R2).
func (s *Service) PreviewReviewsByScope(ctx context.Context, scope string) ([]ReviewHead, error) {
	if strings.TrimSpace(scope) == "" {
		return nil, nil
	}
	return s.classifyScopeReviews(ctx, scope, "", nil)
}

// classifyScopeReviews is classifyHeads over scope's stored reviews.
func (s *Service) classifyScopeReviews(ctx context.Context, scope, tip string, in map[string]bool) ([]ReviewHead, error) {
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil, err
	}
	return s.classifyHeads(ctx, c.PreviewReviews[scope], tip, in), nil
}

// classifyHeads keeps the heads gg can still show exactly (R2): one of tip
// is current; any other needs both its commits (Older). in names commits
// known to exist without a git call.
func (s *Service) classifyHeads(ctx context.Context, heads []ReviewHead, tip string, in map[string]bool) []ReviewHead {
	if len(heads) == 0 {
		return nil
	}
	exists := func(sha string) bool {
		if sha == "" {
			return false
		}
		if in[sha] {
			return true
		}
		_, found, err := s.ResolveRev(ctx, sha+"^{commit}")
		return err == nil && found
	}
	out := make([]ReviewHead, 0, len(heads))
	for _, h := range heads {
		if tip != "" && h.Commit == tip {
			out = append(out, h)
			continue
		}
		base, _, _ := strings.Cut(h.Scope, "..")
		if !exists(h.Commit) || !exists(base) {
			continue
		}
		h.Older = true
		out = append(out, h)
	}
	return out
}

// ReviewOlder reports whether r is a preview review of a tip its preview has
// since moved past — the review view's "older tip". A scope that no longer
// resolves (merged, a side deleted) counts as older: its tip is not current.
func (s *Service) ReviewOlder(ctx context.Context, r Review) bool {
	if r.Preview == "" {
		return false
	}
	set, err := s.NoteScopeResolve(ctx, r.Preview)
	if err != nil || !set.OK() {
		return true
	}
	return set.Tip != r.Commit
}
