package domain

import (
	"context"
	"slices"
	"strings"
)

// Notes on a commit pair.
//
// A pair a..b shows `git diff a b`: commit a on the old side, commit b on the
// new side. The new side is byte for byte the file at b, so a note on it is an
// ordinary committed note on b — the very rule a merge preview follows for its
// source tip. Nothing new is stored, and nothing here forks the preview lane:
// a pair is one more way to BUILD a PreviewNoteSet, and the resolver, the
// loader, the counts and the hunk anchor then serve it unchanged.

// IsPair reports a scope built from two COMMITS (PairNotes) rather than from a
// branch pair (PreviewNotes). The names are what differ: a pair has none, so
// every surface that would print or link them must ask this first.
func (set PreviewNoteSet) IsPair() bool { return set.OK() && set.Source == "" }

// PairNotes is a commit pair's note scope: notes are written to b (new side)
// and gathered along a..b. It reads no saved entry, so an unsaved pair link
// gets the very same scope as a saved one. A commit that is not here yields
// the ZERO set and no error (the preview lane's ruling 6: no set, no badge).
func (s *Service) PairNotes(ctx context.Context, a, b string) (PreviewNoteSet, error) {
	fa, aok, err := s.ResolveRev(ctx, a)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	fb, bok, err := s.ResolveRev(ctx, b)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	if !aok || !bok {
		return PreviewNoteSet{}, nil
	}
	fa, fb = strings.TrimSpace(fa), strings.TrimSpace(fb)
	key := "pair-revlist:" + fa + ":" + fb
	v, err := s.factory.Cache("preview").GetOrLoad(key, func() (any, error) {
		return query(ctx, s, key, func(ctx context.Context) ([]string, error) {
			return s.repo.RevListRange(ctx, fa, fb)
		})
	})
	if err != nil {
		return PreviewNoteSet{}, err
	}
	commits := v.([]string)
	// a..b is EMPTY when b is an ancestor of a (a reversed save). b is still
	// the write target, so it is always a member — or a note just written to
	// it would not show in its own pair. A fresh slice: the cached one is
	// shared.
	if !slices.Contains(commits, fb) {
		commits = append([]string{fb}, commits...)
	}
	return PreviewNoteSet{Tip: fb, Base: fa, Commits: commits}, nil
}
