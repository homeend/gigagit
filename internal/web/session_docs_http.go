package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/homeend/gigagit/internal/agentdocs"
)

// POST /api/session-docs: the page's DELETES of session documents (spec
// 2026-10-18-gg-session-docs §4.4) — an overview document, one note, every
// note on a file — after the page's own confirm. The store's signal tells
// every tab (and a hosting TUI) after; a refused unlink is 409 in the
// TUI's words. The one way a document goes from the page: a close is local.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/session-docs", writeGuard(s.handleSessionDocs))
	})
}

func (s *Server) handleSessionDocs(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Op   string `json:"op"`
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	root := s.docsRoot(r.Context())
	deleted := 0
	switch q.Op {
	case "delete-overview":
		if !agentdocs.ValidID(q.ID) {
			writeErr(w, http.StatusBadRequest, errors.New("want an overview id"))
			return
		}
		if _, ok := s.docs.OverviewIn(root, q.ID); !ok {
			writeErr(w, http.StatusNotFound, errors.New("no overview "+q.ID))
			return
		}
		if !s.docs.RemoveOverviewIn(root, q.ID) {
			writeErr(w, http.StatusConflict, errors.New("could not delete overview "+q.ID))
			return
		}
		deleted = 1
		s.followDocs() // the row goes now, in every tab
	case "delete-note":
		if !agentdocs.ValidID(q.ID) {
			writeErr(w, http.StatusBadRequest, errors.New("want a note id"))
			return
		}
		if _, ok := s.docs.FindNoteIn(root, q.ID); !ok {
			writeErr(w, http.StatusNotFound, errors.New("no note "+q.ID))
			return
		}
		if !s.docs.RemoveNoteIn(root, q.ID) {
			writeErr(w, http.StatusConflict, errors.New("could not delete note "+q.ID))
			return
		}
		deleted = 1
	case "delete-notes":
		if !isGitArgSafe(q.Path) || !filepath.IsLocal(filepath.FromSlash(q.Path)) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
			return
		}
		want := s.docs.NoteCount(root, q.Path)
		if want == 0 {
			writeErr(w, http.StatusNotFound, errors.New("no notes on "+q.Path))
			return
		}
		if deleted = s.docs.ClearPath(root, q.Path); deleted < want {
			writeErr(w, http.StatusConflict, errors.New("could not delete the notes on "+q.Path))
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown op "+q.Op))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "deleted": deleted})
}
