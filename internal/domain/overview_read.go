package domain

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
)

// ErrOverviewNotFound: no permanent overview has that id.
var ErrOverviewNotFound = errors.New("overview not found")

// Overview is one stored permanent overview, as every frontend sees it.
// Kind is a review's three commit states: on the branch (its tip is still
// the commit), was the tip, or on a commit (no branch recorded).
type Overview struct {
	ID, Commit, Branch, Scope, Agent, Title string
	Kind                                    ReviewNoteKind
	Created, Updated                        time.Time
	Doc                                     *notebatch.OverviewDoc
}

// OverviewHead is an overview without its document: what a row needs.
type OverviewHead struct {
	ID, Commit, Branch, Scope, Agent, Title string
	Created                                 time.Time
}

func overviewHead(n model.Note) OverviewHead {
	return OverviewHead{ID: n.ID, Commit: n.Address.Commit, Branch: n.Address.Branch, Scope: n.Preview,
		Agent: n.Author, Title: strings.TrimSpace(n.Summary), Created: n.Created}
}

// overviewNotes is every overview note (roots), newest first.
func (s *Service) overviewNotes(ctx context.Context) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := loadParts(st, notes.PartCommits, notes.PartPreviews)
	if err != nil {
		return nil, err
	}
	var out []model.Note
	for _, n := range all {
		if !n.IsReply() && n.IsOverviewNote() {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out, nil
}

func (s *Service) overviewOf(ctx context.Context, n model.Note, tips map[string]string) Overview {
	h := overviewHead(n)
	b := h.Branch
	tip, ok := tips[b]
	if !ok && b != "" {
		tip = s.branchTip(ctx, b)
		tips[b] = tip
	}
	o := Overview{ID: h.ID, Commit: h.Commit, Branch: b, Scope: h.Scope, Agent: h.Agent, Title: h.Title,
		Kind: ReviewKindOf(b, h.Commit, tip), Created: n.Created, Updated: n.Updated}
	if doc, err := notebatch.ParseOverview([]byte(n.Rationale)); err == nil {
		o.Doc = &doc
	}
	return o
}

// Overviews is every stored overview (commit and PR), newest first.
func (s *Service) Overviews(ctx context.Context) ([]Overview, error) {
	ns, err := s.overviewNotes(ctx)
	tips := map[string]string{}
	out := make([]Overview, 0, len(ns))
	for _, n := range ns {
		out = append(out, s.overviewOf(ctx, n, tips))
	}
	return out, err
}

// Overview is one stored overview by id.
func (s *Service) Overview(ctx context.Context, id string) (Overview, error) {
	ns, err := s.overviewNotes(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, n := range ns {
		if n.ID == id {
			return s.overviewOf(ctx, n, map[string]string{}), nil
		}
	}
	return Overview{}, ErrOverviewNotFound
}

// OverviewsForCommit is the commit overviews stored on sha (a PR's are its
// PR's, never its head commit's — R10), newest first.
func (s *Service) OverviewsForCommit(ctx context.Context, sha string) ([]Overview, error) {
	ns, err := s.overviewNotes(ctx)
	tips := map[string]string{}
	var out []Overview
	for _, n := range ns {
		if n.Address.Commit == sha && n.Preview == "" {
			out = append(out, s.overviewOf(ctx, n, tips))
		}
	}
	return out, err
}

// RemoveOverview deletes overview id; ErrOverviewNotFound when id is not one.
func (s *Service) RemoveOverview(ctx context.Context, id string) error {
	if _, err := s.Overview(ctx, id); err != nil {
		return err
	}
	return s.NoteRemove(ctx, id)
}
