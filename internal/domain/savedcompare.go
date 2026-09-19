package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/savedcompare"
)

// ErrSavedComparesDisabled is returned when there is no state directory to
// store comparisons in.
var ErrSavedComparesDisabled = errors.New("saved comparisons: no state directory available")

// ErrSavedCompareNotFound / ErrSavedCompareExists WRAP the store's errors so
// frontends (which cannot import internal/savedcompare) can errors.Is them.
var (
	ErrSavedCompareNotFound = fmt.Errorf("%w", savedcompare.ErrNotFound)
	ErrSavedCompareExists   = fmt.Errorf("%w", savedcompare.ErrExists)
)

// SavedCompare is one stored comparison as a FRONTEND sees it: link TEXTS,
// which is what a frontend pastes, prints and hands back. Right == "" means a
// saved SET (a merge preview), not a pair.
//
// The texts rather than model.Link values, deliberately: every consumer of
// this type — `gg compare --list`, the TUI palette, the web dialog — wants
// the spelling a user could copy, and the one that round-trips back through
// ParseLink unchanged.
type SavedCompare struct {
	ID      string
	Left    string
	Right   string
	Label   string
	Created time.Time
}

// IsSet reports whether this is a bounded SET rather than a pair.
func (c SavedCompare) IsSet() bool { return c.Right == "" }

func savedCompareFromEntry(e savedcompare.Entry) SavedCompare {
	out := SavedCompare{ID: e.ID, Left: e.Left.String(), Label: e.Label, Created: e.Created}
	if e.Right != nil {
		out.Right = e.Right.String()
	}
	return out
}

// SavedCompareAdd stores a comparison. right may be empty, which stores a
// bounded SET rather than a pair. Both halves are PARSED here, so the store
// can never hold a row that will not read back.
func (s *Service) SavedCompareAdd(ctx context.Context, left, right, label string) (SavedCompare, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return SavedCompare{}, ErrSavedComparesDisabled
	}
	l, err := model.ParseLink(left)
	if err != nil {
		return SavedCompare{}, err
	}
	e := savedcompare.Entry{Left: l, Label: label}
	if right != "" {
		r, err := model.ParseLink(right)
		if err != nil {
			return SavedCompare{}, err
		}
		e.Right = &r
	}
	stored, err := st.Add(e)
	if errors.Is(err, savedcompare.ErrExists) {
		return savedCompareFromEntry(stored), ErrSavedCompareExists
	}
	if err != nil {
		return SavedCompare{}, err
	}
	return savedCompareFromEntry(stored), nil
}

// SavedCompareList returns BOTH shapes in insertion order — unlike
// PreviewList, which shows only the merge previews among them.
func (s *Service) SavedCompareList(ctx context.Context) ([]SavedCompare, error) {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return nil, ErrSavedComparesDisabled
	}
	es, err := st.List()
	if err != nil {
		return nil, err
	}
	out := make([]SavedCompare, 0, len(es))
	for _, e := range es {
		out = append(out, savedCompareFromEntry(e))
	}
	return out, nil
}

// SavedCompareGet finds a record by id, else by exact label (first match) —
// the same order PreviewGet uses.
func (s *Service) SavedCompareGet(ctx context.Context, idOrLabel string) (SavedCompare, error) {
	cs, err := s.SavedCompareList(ctx)
	if err != nil {
		return SavedCompare{}, err
	}
	for _, c := range cs {
		if c.ID == idOrLabel {
			return c, nil
		}
	}
	for _, c := range cs {
		if c.Label == idOrLabel {
			return c, nil
		}
	}
	return SavedCompare{}, ErrSavedCompareNotFound
}

// SavedCompareRemove deletes by id. Unlike PreviewRemove it accepts BOTH
// shapes: this is the surface that owns every stored comparison, where the
// preview surfaces own only the subset they can show.
func (s *Service) SavedCompareRemove(ctx context.Context, id string) error {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return ErrSavedComparesDisabled
	}
	if err := st.Remove(id); errors.Is(err, savedcompare.ErrNotFound) {
		return ErrSavedCompareNotFound
	} else {
		return err
	}
}

// SavedCompareRename relabels by id, both shapes.
func (s *Service) SavedCompareRename(ctx context.Context, id, label string) error {
	st := s.savedCompareStore(ctx)
	if st == nil {
		return ErrSavedComparesDisabled
	}
	if err := st.Rename(id, label); errors.Is(err, savedcompare.ErrNotFound) {
		return ErrSavedCompareNotFound
	} else {
		return err
	}
}
