package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/savedcompare"
)

// ErrPairNotFound / ErrPairExists WRAP the store's errors, like their preview
// twins, so frontends can errors.Is them without importing the store.
var (
	ErrPairNotFound = fmt.Errorf("%w", savedcompare.ErrNotFound)
	ErrPairExists   = fmt.Errorf("%w", savedcompare.ErrExists)
)

// CommitPair is a saved commits diff: the change-set A..B between two FROZEN
// commits. It is the second set-shaped savedcompare row a Previews surface can
// show; a merge preview is the first. A holds the old side, B the new.
type CommitPair struct {
	ID, Label string
	A, B      string
	Created   time.Time
}

// DefaultLabel is "<a7>..<b7>".
func (p CommitPair) DefaultLabel() string { return shortSha(p.A) + ".." + shortSha(p.B) }

func shortSha(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// fullSha reports whether s is a whole object id (sha-1 or sha-256), which is
// the only spelling PairAdd ever stores.
func fullSha(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// pairFromEntry is previewFromEntry's twin: it reads a set-shaped entry back
// as a commit pair and reports false for everything PairAdd did not produce —
// a comparison (Right set), a merge preview, and a pair set somebody stored by
// NAME, by short sha, narrowed to a path or carrying a hint. Those are
// legitimate rows this surface does not promise to render.
func pairFromEntry(e savedcompare.Entry) (CommitPair, bool) {
	if e.Right != nil {
		return CommitPair{}, false
	}
	pr := e.Left.Target.Pair
	if pr == nil || e.Left.Path != "" || e.Left.Hint != (model.LinkHint{}) {
		return CommitPair{}, false
	}
	if !fullSha(pr.A) || !fullSha(pr.B) {
		return CommitPair{}, false
	}
	return CommitPair{ID: e.ID, Label: e.Label, A: pr.A, B: pr.B, Created: e.Created}, true
}

// PairAdd FREEZES a and b to full shas (a branch name stops following its
// branch here) and stores the change-set a..b. A duplicate returns the
// EXISTING row with ErrPairExists so a frontend can focus it.
func (s *Service) PairAdd(ctx context.Context, a, b, label string) (CommitPair, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return CommitPair{}, ErrSavedComparesDisabled
	}
	var shas [2]string
	for i, rev := range []string{a, b} {
		sha, ok, err := s.ResolveRev(ctx, rev)
		if err != nil {
			return CommitPair{}, err
		}
		if !ok {
			return CommitPair{}, fmt.Errorf("pair: %q is not a commit", rev)
		}
		shas[i] = sha
	}
	if shas[0] == shas[1] {
		return CommitPair{}, errors.New("pair: a pair needs two different commits")
	}
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return CommitPair{}, err
	}
	// Through String→ParseLink, like entryFromPreview: the store must never
	// hold a row that will not read back.
	l, err := model.ParseLink(model.Link{Repo: repo, Target: model.LinkTarget{
		State: model.StateCommitted,
		Pair:  &model.LinkPair{A: shas[0], B: shas[1]},
	}}.String())
	if err != nil {
		return CommitPair{}, err
	}
	if label == "" {
		label = CommitPair{A: shas[0], B: shas[1]}.DefaultLabel()
	}
	stored, err := st.Add(savedcompare.Entry{Left: l, Label: label})
	p, ok := pairFromEntry(stored)
	if !ok {
		return CommitPair{}, fmt.Errorf("pair: stored entry %q is not a commit pair", stored.ID)
	}
	if errors.Is(err, savedcompare.ErrExists) {
		return p, ErrPairExists
	}
	return p, err
}

// PairList returns the commit pairs among the saved comparisons, in insertion
// order.
func (s *Service) PairList(ctx context.Context) ([]CommitPair, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return nil, ErrSavedComparesDisabled
	}
	es, err := st.List()
	if err != nil {
		return nil, err
	}
	out := make([]CommitPair, 0, len(es))
	for _, e := range es {
		if p, ok := pairFromEntry(e); ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// PairGet finds a pair by id, else by exact label (first match).
func (s *Service) PairGet(ctx context.Context, idOrLabel string) (CommitPair, error) {
	ps, err := s.PairList(ctx)
	if err != nil {
		return CommitPair{}, err
	}
	for _, p := range ps {
		if p.ID == idOrLabel {
			return p, nil
		}
	}
	for _, p := range ps {
		if p.Label == idOrLabel {
			return p, nil
		}
	}
	return CommitPair{}, ErrPairNotFound
}

// PairRename relabels a PAIR; an id naming any other shape is not found, the
// rule PreviewRename applies from its side.
func (s *Service) PairRename(ctx context.Context, id, label string) error {
	if _, err := s.PairGet(ctx, id); err != nil {
		return err
	}
	if err := s.SavedCompareRename(ctx, id, label); errors.Is(err, ErrSavedCompareNotFound) {
		return ErrPairNotFound
	} else {
		return err
	}
}

// PairRemove deletes a PAIR by id.
func (s *Service) PairRemove(ctx context.Context, id string) error {
	if _, err := s.PairGet(ctx, id); err != nil {
		return err
	}
	if err := s.SavedCompareRemove(ctx, id); errors.Is(err, ErrSavedCompareNotFound) {
		return ErrPairNotFound
	} else {
		return err
	}
}
