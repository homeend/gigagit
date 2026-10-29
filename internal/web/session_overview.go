package web

// Opening a session overview link (gg://<worktree>?overview=<id>, no
// address) in gg web — the TUI's steerNavigateSessionOverview: the
// overview is the shown worktree's session document of that id, found by
// id only, and it opens as gg session files focus opens a document. A
// miss leaves the navigate as it was: the page then looks for a permanent
// overview of that id.

import (
	"context"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

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

// landSessionOverview turns a session overview's navigate into the
// file_focus the page lands — focused in the server's list first, as
// steerFileFocus does — when this worktree holds it; any other command,
// and a miss, come back unchanged.
func (s *Server) landSessionOverview(ctx context.Context, w steerWire) steerWire {
	if !isSessionOverviewNav(w) {
		return w
	}
	f, ok := s.findSessionOverview(ctx, w.HintID)
	if !ok {
		return w
	}
	wt := s.service().Root()
	s.ofs.focus(wt, f.ID, "")
	s.broadcastOpenFiles(wt, "")
	return steerWire{Cmd: "file_focus", FileID: f.ID}
}
