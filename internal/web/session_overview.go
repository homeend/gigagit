package web

// Opening a session overview link (gg://<worktree>?overview=<id>, no
// address) in gg web — the TUI's steerNavigateSessionOverview: the
// overview is the shown worktree's session document of that id, found by
// id only, and it opens as gg session files focus opens a document. A
// miss leaves the navigate as it was: the page then looks for a permanent
// overview of that id.

import (
	"context"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/session-overview/link", s.handleSessionOverviewLink)
	})
}

// GET /api/session-overview/link?id=<id> is a session overview's gg:// link
// (the TUI's overviewLinkRow): built from the worktree the store is saved
// to, so a memory-only overview has none.
func (s *Server) handleSessionOverviewLink(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !agentdocs.ValidID(id) {
		writeErr(w, http.StatusBadRequest, errors.New("a valid overview id is required"))
		return
	}
	root := s.docsRoot(readCtx(r))
	wt := s.docs.AttachedWorktree(root)
	if wt == "" {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("this overview is not saved to a worktree — no link"))
		return
	}
	if _, ok := s.docs.OverviewIn(root, id); !ok {
		writeErr(w, http.StatusNotFound, errors.New("overview "+id+" no longer exists"))
		return
	}
	link, err := domain.SessionOverviewLink(wt, id)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSON(w, map[string]string{"link": link})
}

// isSessionOverviewNav reports whether w is a session overview's navigate:
// the overview hint and no file, commit or target.
func isSessionOverviewNav(w steerWire) bool {
	return w.Cmd == "navigate" && w.HintKind == model.OverviewHintKind && w.File == "" && w.Commit == "" && w.State == ""
}

// findSessionOverview is the shown worktree's open overview document id —
// never a file whose path reads like it. One written a moment ago may not
// be listed yet (the store's watch has not fired), so a miss reads the
// directory once.
func (s *Server) findSessionOverview(ctx context.Context, id string) (steer.OpenFile, bool) {
	wt := s.service().Root()
	if f, ok := s.ofs.overviewByID(wt, id); ok {
		return f, true
	}
	if !agentdocs.ValidID(id) {
		return steer.OpenFile{}, false
	}
	s.docs.Reload(s.docsRoot(ctx))
	s.followDocs()
	return s.ofs.overviewByID(wt, id)
}

// sessionOverviewFocus turns a session overview's navigate into the
// file_focus the page lands when this worktree holds it; any other command,
// and a miss, come back unchanged. A lookup only — its callers are GETs, and
// the page's own open (openViewer → POST focus) moves the overview first in
// the list.
func (s *Server) sessionOverviewFocus(ctx context.Context, w steerWire) steerWire {
	if !isSessionOverviewNav(w) {
		return w
	}
	f, ok := s.findSessionOverview(ctx, w.HintID)
	if !ok {
		return w
	}
	return steerWire{Cmd: "file_focus", FileID: f.ID}
}
