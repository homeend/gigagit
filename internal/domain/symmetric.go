package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// A SYMMETRIC merge preview is two merge previews against one base, compared:
// "what A would bring into base" against "what B would bring into base" — two
// branches that fix the same thing. It is not a store kind. It is an ordinary
// two-link saved comparison whose halves are both whole-tree three-dot
// preview links with the SAME target, and SymmetricOf is the one place that
// shape is recognised. A comparison saved by hand from two such previews is
// symmetric too: it is the same data.
//
// The links hold branch NAMES, so the comparison is live: every open resolves
// the three tips again.

// Symmetric names a symmetric preview's three branches.
type Symmetric struct{ A, B, Base string }

// DefaultLabel is the row name when none is given. No arrow glyph: ↔ reads as
// a left arrow in a monospace cell.
func (s Symmetric) DefaultLabel() string {
	return fmt.Sprintf("%s vs %s (base: %s)", s.A, s.B, s.Base)
}

// SymmetricOf reports whether c is a symmetric preview and names its branches.
//
// A PARSED local-form link keeps its file path inside Repo.Abs (see
// model.LinkRepo), so two file-level links into the same file of one local
// checkout cannot be told from whole-tree ones here; no producer saves such a
// pair, and one that did would still open as the comparison it spells.
func SymmetricOf(c SavedCompare) (Symmetric, bool) {
	if c.IsSet() {
		return Symmetric{}, false
	}
	l, errL := model.ParseLink(c.Left)
	r, errR := model.ParseLink(c.Right)
	if errL != nil || errR != nil {
		return Symmetric{}, false
	}
	lp, rp := wholePreview(l), wholePreview(r)
	if lp == nil || rp == nil || l.Repo != r.Repo || lp.Target != rp.Target || lp.Source == rp.Source {
		return Symmetric{}, false
	}
	return Symmetric{A: lp.Source, B: rp.Source, Base: lp.Target}, true
}

// wholePreview is l's preview when l addresses a whole merge preview: no
// path, line, hunk or hint.
func wholePreview(l model.Link) *model.LinkPreview {
	if l.Path != "" || l.Line != 0 || l.Hunk != 0 || l.Hint.Kind != "" {
		return nil
	}
	return l.Target.Preview
}

// SymmetricPreviewAdd saves the comparison @base...a against @base...b. A
// duplicate hands back the stored entry with ErrSavedCompareExists, which
// frontends treat as success (they open it).
func (s *Service) SymmetricPreviewAdd(ctx context.Context, a, b, base, label string) (SavedCompare, error) {
	a, b, base = strings.TrimSpace(a), strings.TrimSpace(b), strings.TrimSpace(base)
	switch {
	case a == "" || b == "" || base == "":
		return SavedCompare{}, errors.New("symmetric preview: needs two branches and a base")
	case a == b:
		return SavedCompare{}, errors.New("symmetric preview: the two branches are the same")
	case base == a || base == b:
		return SavedCompare{}, errors.New("symmetric preview: the base must differ from both branches")
	}
	for _, name := range []string{a, b, base} {
		if !model.LinkRefOK(name) {
			return SavedCompare{}, fmt.Errorf("symmetric preview: %q cannot be written in a gg:// link", name)
		}
		if _, ok, err := s.ResolveRev(ctx, name); err != nil {
			return SavedCompare{}, err
		} else if !ok {
			return SavedCompare{}, fmt.Errorf("symmetric preview: %q is not a branch or commit", name)
		}
	}
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return SavedCompare{}, err
	}
	if label == "" {
		label = Symmetric{A: a, B: b, Base: base}.DefaultLabel()
	}
	return s.SavedCompareAdd(ctx, previewLinkText(repo, a, base), previewLinkText(repo, b, base), label)
}

// previewLinkText is the whole-tree link of the merge preview "source into
// target", spelled target first — the one construction entryFromPreview and
// SymmetricPreviewAdd share.
func previewLinkText(repo model.LinkRepo, source, target string) string {
	return model.Link{Repo: repo, Target: model.LinkTarget{
		State:   model.StateCommitted,
		Preview: &model.LinkPreview{Source: source, Target: target},
	}}.String()
}
