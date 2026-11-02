package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
)

// POST /api/session-overview/memory {title, text}: ctrl+\'s n, a memory, plain
// document of the worktree on screen (spec 2026-11-02-memory-docs-design).
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/session-overview/memory", writeGuard(s.handleMemdocCreate))
	})
}

func (s *Server) handleMemdocCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*agentdocs.MaxOverviewBytes)).Decode(&req); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("the text is over 64 KiB"))
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("give it a title"))
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("give it a text"))
		return
	}
	root, wt := s.docsRoot(readCtx(r)), s.service().Root()
	o, err := s.docs.AddMemoryOverview(root, wt, agentdocs.MintID(clock.Now(), nil), req.Title, req.Text, "", true)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	s.followDocs() // listed before the page opens it
	link, err := domain.SessionOverviewLink(wt, o.ID)
	if err != nil {
		link = "" // a worktree path a link cannot spell: the document stands
	}
	writeJSON(w, map[string]string{"id": o.ID, "link": link})
}
