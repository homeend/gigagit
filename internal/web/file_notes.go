package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/agentdocs"
)

// POST /api/file-notes {op:"dismiss", id}: the viewer's d and its dismiss
// button. The store's signal tells every tab — and a hosting TUI — after.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/file-notes", writeGuard(s.handleFileNotes))
	})
}

func (s *Server) handleFileNotes(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Op string `json:"op"`
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if q.Op != "dismiss" || !agentdocs.ValidID(q.ID) {
		writeErr(w, http.StatusBadRequest, errors.New("want op dismiss and a note id"))
		return
	}
	root := s.docsRoot(r.Context())
	if _, ok := s.docs.FindNoteIn(root, q.ID); !ok || !s.docs.RemoveNoteIn(root, q.ID) {
		writeErr(w, http.StatusNotFound, errors.New("no note "+q.ID))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
