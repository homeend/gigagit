package web

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/markdown"
)

// GET /api/overview?id=f7: an agent's overview for the viewer's document
// mode — the parsed tree (anchors as kind "anchor", numbered in document
// order) and the anchors, checked now. A check that changed a flag signals
// the store, so every tab and a hosting TUI repaint.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/overview", s.handleOverview)
	})
}

var fileIDRE = regexp.MustCompile(`^f[0-9]{1,18}$`)

// overviewAnchor is one anchor on the wire. A note anchor whose note is
// there carries the note's file and lines: the page has no note index.
type overviewAnchor struct {
	Dest    string `json:"dest"`
	Path    string `json:"path,omitempty"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
	Note    string `json:"note,omitempty"`
	Missing bool   `json:"missing"`
	Ref     string `json:"ref"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !fileIDRE.MatchString(id) {
		writeErr(w, http.StatusBadRequest, errors.New("want an overview id"))
		return
	}
	root := s.docsRoot(r.Context())
	if o, ok := s.docs.Overview(id); !ok || o.Root != root {
		writeErr(w, http.StatusNotFound, errors.New("no overview "+id))
		return
	}
	s.docs.CheckAnchors(id)
	// The stamp is taken BEFORE the copy: a change in between leaves the
	// tab a copy newer than its stamp, which the next fan-out refreshes —
	// never a stamp newer than its copy, which would skip that refresh.
	stamp := s.docs.OverviewStamp(id)
	o, ok := s.docs.Overview(id)
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no overview "+id))
		return
	}
	doc, _ := agentdocs.ParseOverview(o.Text)
	anchors := make([]overviewAnchor, len(o.Anchors))
	for i, a := range o.Anchors {
		wa := overviewAnchor{Dest: a.Dest, Path: a.Path, Start: a.Start, End: a.End, Note: a.Note,
			Missing: a.Missing, Ref: agentdocs.AnchorReference(o, a)}
		if a.Note != "" && !a.Missing {
			if n, ok := s.docs.FindNote(a.Note); ok && n.Root == root {
				wa.Path, wa.Start, wa.End = n.Path, n.Start, n.End
			}
		}
		anchors[i] = wa
	}
	writeJSON(w, struct {
		ID      string           `json:"id"`
		Title   string           `json:"title"`
		Text    string           `json:"text"`
		Stamp   string           `json:"stamp"`
		Blocks  []markdown.Block `json:"blocks"`
		Anchors []overviewAnchor `json:"anchors"`
	}{o.ID, o.Title, o.Text, stamp, doc.Blocks, anchors})
}
