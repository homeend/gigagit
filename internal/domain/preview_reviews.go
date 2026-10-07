package domain

import (
	"context"
	"strings"
)

// PreviewReviews are the reviews written for set (spec R2), newest first:
// current — its tip is the set's tip; older (Older) — the tip moved on but
// both commits the review read still exist, so the review view can open
// exactly what was reviewed; gone — a reviewed commit is no longer in the
// repository: omitted (View all notes still lists it). Matching is by the
// scope NAME, so a removed and re-added saved row, or a preview opened only
// from a link, finds the same reviews. A tip still in the set's commit list
// costs no git call; only a rewritten one is looked up.
func (s *Service) PreviewReviews(ctx context.Context, set PreviewNoteSet) ([]ReviewHead, error) {
	sc := set.scope()
	if sc == "" {
		return nil, nil
	}
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil, err
	}
	heads := c.PreviewReviews[sc]
	if len(heads) == 0 {
		return nil, nil
	}
	in := set.commitSet()
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
		if h.Commit == set.Tip {
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
	return out, nil
}
