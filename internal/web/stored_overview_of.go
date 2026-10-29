package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/agentdocs"
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
	st, title, err := s.readStored(ctx, s.service(), q.Kind, q.ID)
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
// a review by its label, a permanent overview by its title). The text is
// parsed, never resolved: the row needs the anchors as written, so no file
// of the commit is read (resolving is the viewer's read).
func (s *Server) readStored(ctx context.Context, svc *domain.Service, kind, id string) (ofStored, string, error) {
	st := ofStored{Kind: kind, ID: id}
	var text, title string
	if kind == "overview" {
		o, err := svc.Overview(ctx, id)
		if err != nil {
			return st, "", err
		}
		if o.Doc == nil {
			return st, "", domain.ErrNoOverview
		}
		text, st.Tip, title = o.Doc.Overview, o.Commit, o.Title
	} else {
		rv, err := svc.Review(ctx, id)
		if err != nil {
			return st, "", err
		}
		if !rv.HasOverview() {
			return st, "", domain.ErrNoOverview
		}
		text, title = rv.Doc.Overview, reviewLabel(rv)
		if rv.Kind != domain.ReviewOnWorktree {
			_, st.Tip, _ = svc.ReviewRevs(ctx, rv)
		}
	}
	_, anchors := agentdocs.ParseOverview(text)
	st.Anchors = len(anchors)
	st.Children = storedChildren(anchors, st.Tip)
	return st, "Overview: " + title, nil
}

// storedChildren is one child per distinct path a PATH anchor names, in
// the order first named, each opening at tip (a stored overview's note
// anchors name nothing — the TUI's overviewChildren).
func storedChildren(as []agentdocs.Anchor, tip string) []ofChild {
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

// dropGoneOverviews takes the permanent overviews no longer among ids (a
// successful counts read's every id) out of the served worktree's list and
// tells the tabs — deleted behind the page, as the TUI's dropGoneOverviews.
func (s *Server) dropGoneOverviews(ids []string) {
	live := make(map[string]bool, len(ids))
	for _, id := range ids {
		live[id] = true
	}
	wt := s.service().Root()
	// The update names what went (closed): a tab showing it says it was
	// deleted, not closed in another tab.
	if gone := s.ofs.dropGoneStored(wt, "overview", live); len(gone) > 0 {
		if h := s.liveHubRef(); h != nil {
			h.fanOut(liveMsg{Changed: []string{}, Reason: "open_files", Files: s.pageList(wt), Closed: gone})
		}
	}
}
