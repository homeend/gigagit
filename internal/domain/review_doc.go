package domain

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// The review view's reads. A structured review (Review.Doc) is one stored note;
// its line notes are built from the document at READ time, like a forge
// comment (forgeNotesFor), carry read-only "review:<review id>:<n>" ids and are
// never stored. <n> numbers the document's notes in order.

// canonicalReview is text in the review document's canonical form when it
// parses as one, else text unchanged.
func canonicalReview(text string) string {
	if doc, err := notebatch.ParseReview([]byte(text)); err == nil {
		return string(doc.Canonical())
	}
	return text
}

// ReviewOtherNote is a document note the review view cannot place on a line:
// its path is not among the reviewed files, or its line is past the end of
// that side of the file.
type ReviewOtherNote struct {
	Path    string
	Side    model.NoteSide
	Range   [2]int
	Summary string
}

// reviewRevs are the two revisions a review compares: base (the old side) and
// tip (the new side, the review's commit). A single-commit review — its scope
// is "<commit>^..<commit>" or unreadable — compares against the commit's
// parent.
func (s *Service) reviewRevs(ctx context.Context, r Review) (base, tip string, isRange bool) {
	tip, base = r.Commit, r.Commit+"^"
	a, b, ok := strings.Cut(r.Scope, "..")
	b = strings.TrimPrefix(b, ".")
	if !ok || a == "" || b == "" {
		return base, tip, false
	}
	ra, okA, errA := s.ResolveRev(ctx, a)
	rb, okB, errB := s.ResolveRev(ctx, b)
	if errA != nil || errB != nil || !okA || !okB || strings.TrimSpace(rb) != r.Commit {
		return base, tip, false
	}
	ra = strings.TrimSpace(ra)
	if pa, okP, errP := s.ResolveRev(ctx, r.Commit+"^"); errP == nil && okP && strings.TrimSpace(pa) == ra {
		return base, tip, false
	}
	return ra, tip, true
}

// ReviewRevs are the two revisions review r compares (see reviewRevs): the
// review view opens a single-commit review as that commit's files and a range
// review as a compare of base..tip.
func (s *Service) ReviewRevs(ctx context.Context, r Review) (base, tip string, isRange bool) {
	return s.reviewRevs(ctx, r)
}

// ReviewFiles are the files the review view lists: the commit's changed files,
// or for a range review the files that differ across its range.
func (s *Service) ReviewFiles(ctx context.Context, r Review) ([]model.CommitFile, error) {
	base, tip, isRange := s.reviewRevs(ctx, r)
	if !isRange {
		return s.CommitFiles(ctx, tip)
	}
	left, err := model.CommitEndpoint(base)
	if err != nil {
		return nil, err
	}
	right, err := model.CommitEndpoint(tip)
	if err != nil {
		return nil, err
	}
	return s.CompareFiles(ctx, left, right)
}

func reviewPath(p string) string { return strings.TrimPrefix(filepath.ToSlash(p), "./") }

func reviewSide(side string) model.NoteSide {
	if side == "old" {
		return model.NoteSideOld
	}
	return model.NoteSideNew
}

// fitsSide reports whether rng lies inside lines (nil = the side is absent).
func fitsSide(lines []string, rng [2]int) bool {
	return lines != nil && rng[0] >= 1 && rng[1] <= len(lines)
}

// ReviewNotesFor is the review's notes on path, resolved against d — the diff
// the review view shows for that file. A note whose line is past the end of
// its side is left out here and listed by ReviewOtherNotes. A prose review has
// none.
func (s *Service) ReviewNotesFor(ctx context.Context, reviewID, path string, d Diff) ([]ResolvedNote, error) {
	r, err := s.Review(ctx, reviewID)
	if err != nil || r.Doc == nil {
		return nil, err
	}
	oldLines, newLines := diffSideLines(d)
	want := reviewPath(path)
	var out []ResolvedNote
	i := 0
	for _, f := range r.Doc.Files {
		for _, dn := range f.Notes {
			id := fmt.Sprintf("%s%s:%d", model.ReviewNoteIDPrefix, r.ID, i)
			i++
			if reviewPath(f.Path) != want {
				continue
			}
			side := reviewSide(dn.Side)
			lines := newLines
			if side == model.NoteSideOld {
				lines = oldLines
			}
			if !fitsSide(lines, dn.Range) {
				continue
			}
			n := model.Note{ID: id, Source: model.NoteSourceAgent, Author: r.Agent,
				Address: model.FileAddress{State: model.StateCommitted, Commit: r.Commit, Path: path},
				Side:    side, Range: dn.Range, Summary: dn.Summary, Rationale: dn.Rationale,
				Created: r.Created, Updated: r.Updated}
			for _, kv := range dn.Meta {
				n.Tags = append(n.Tags, kv.Key+": "+kv.Value)
			}
			out = append(out, ResolvedNote{Note: n, Status: model.NoteActive, Range: dn.Range})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Note.Side != out[b].Note.Side {
			return out[a].Note.Side == model.NoteSideNew
		}
		return out[a].Range[0] < out[b].Range[0]
	})
	return out, nil
}

// reviewSplit sorts the document's notes into per-file counts (notes the view
// places on a line) and the other notes, reading each named file's two sides
// once.
func (s *Service) reviewSplit(ctx context.Context, reviewID string) (map[string]int, []ReviewOtherNote, error) {
	r, err := s.Review(ctx, reviewID)
	if err != nil || r.Doc == nil {
		return nil, nil, err
	}
	files, err := s.ReviewFiles(ctx, r)
	if err != nil {
		return nil, nil, err
	}
	inView := map[string]bool{}
	for _, f := range files {
		inView[reviewPath(f.Path)] = true
	}
	base, tip, _ := s.reviewRevs(ctx, r)
	type sides struct{ old, new []string }
	read := map[string]sides{}
	sideOf := func(path string) sides {
		if sd, ok := read[path]; ok {
			return sd
		}
		sd := sides{old: s.revLines(ctx, base, path), new: s.revLines(ctx, tip, path)}
		read[path] = sd
		return sd
	}
	counts := map[string]int{}
	var other []ReviewOtherNote
	for _, f := range r.Doc.Files {
		p := reviewPath(f.Path)
		for _, dn := range f.Notes {
			side := reviewSide(dn.Side)
			if inView[p] {
				sd := sideOf(p)
				lines := sd.new
				if side == model.NoteSideOld {
					lines = sd.old
				}
				if fitsSide(lines, dn.Range) {
					counts[p]++
					continue
				}
			}
			other = append(other, ReviewOtherNote{Path: p, Side: side, Range: dn.Range, Summary: dn.Summary})
		}
	}
	return counts, other, nil
}

// revLines is path's text at rev split into lines; nil when rev lacks it.
func (s *Service) revLines(ctx context.Context, rev, path string) []string {
	b, err := s.ShowFile(ctx, rev, path)
	if err != nil {
		return nil
	}
	text := strings.TrimSuffix(string(b), "\n")
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

// ReviewFileCounts is, per file the review view lists, how many of the
// review's notes it places on a line of that file. Files without notes are
// absent.
func (s *Service) ReviewFileCounts(ctx context.Context, reviewID string) (map[string]int, error) {
	counts, _, err := s.reviewSplit(ctx, reviewID)
	return counts, err
}

// ReviewOtherNotes are the review's notes the view cannot place on a line, in
// document order: on a file the review does not change, or past its end.
func (s *Service) ReviewOtherNotes(ctx context.Context, reviewID string) ([]ReviewOtherNote, error) {
	_, other, err := s.reviewSplit(ctx, reviewID)
	return other, err
}
