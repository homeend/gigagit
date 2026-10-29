package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// A stored overview in the open-files list (spec
// 2026-10-29-web-stored-overview-tree): a tab that opens a review's or a
// commit's permanent overview registers it, so every tab's ctrl+\ tree
// lists it — the TUI's srcReviewOverview / srcCommitOverview open files.
// The server reads the document; the wire names only its kind and id.

// openStoredOverview is op "stored": read q.Kind's q.ID and list it (or
// refresh its listed entry), shown by q.Tab. The status is the error's.
func (s *Server) openStoredOverview(ctx context.Context, wt string, q ofReq) (steer.OpenFile, int, error) {
	if (q.Kind != "review" && q.Kind != "overview") || !reviewIDRE.MatchString(q.ID) {
		return steer.OpenFile{}, http.StatusBadRequest, errors.New("want a review or overview id")
	}
	st, title, err := s.readStored(ctx, q.Kind, q.ID)
	switch {
	case errors.Is(err, domain.ErrReviewNotFound), errors.Is(err, domain.ErrOverviewNotFound), errors.Is(err, domain.ErrNoOverview):
		return steer.OpenFile{}, http.StatusNotFound, err
	case err != nil:
		return steer.OpenFile{}, http.StatusInternalServerError, err
	}
	src := "review-overview"
	if q.Kind == "overview" {
		src = "commit-overview"
	}
	k := ofKey{Src: src, Rev: q.ID, Path: "overview-" + q.ID + ".md"}
	return s.ofs.openStored(wt, k, st, title, q.Tab), http.StatusOK, nil
}

// readStored reads stored overview id of kind: its document reference,
// anchor count and children, and its row title (the TUI's "Overview: %s" —
// a review by its label, a permanent overview by its title).
func (s *Server) readStored(ctx context.Context, kind, id string) (ofStored, string, error) {
	svc := s.service()
	st := ofStored{Kind: kind, ID: id}
	var od domain.OverviewDoc
	var title string
	if kind == "overview" {
		o, err := svc.Overview(ctx, id)
		if err != nil {
			return st, "", err
		}
		if od, err = svc.OverviewDocAt(ctx, id); err != nil {
			return st, "", err
		}
		st.Tip, title = o.Commit, o.Title
	} else {
		rv, err := svc.Review(ctx, id)
		if err != nil {
			return st, "", err
		}
		if od, err = svc.ReviewOverview(ctx, id); err != nil {
			return st, "", err
		}
		if rv.Kind != domain.ReviewOnWorktree {
			_, st.Tip, _ = svc.ReviewRevs(ctx, rv)
		}
		title = reviewLabel(rv)
	}
	st.Anchors = len(od.Anchors)
	st.Children = storedChildren(od.Anchors, st.Tip)
	return st, "Overview: " + title, nil
}

// storedChildren is one child per distinct path a PATH anchor names, in
// the order first named, each opening at tip (a stored overview's note
// anchors name nothing — the TUI's overviewChildren).
func storedChildren(as []domain.OverviewAnchor, tip string) []ofChild {
	seen := map[string]bool{}
	var out []ofChild
	for i, a := range as {
		if a.Note != "" || a.Path == "" || seen[a.Path] {
			continue
		}
		seen[a.Path] = true
		out = append(out, ofChild{Path: a.Path, Anchor: i, Rev: tip})
	}
	return out
}
