// Package web is gg's read-only browser frontend probe: an embedded HTTP
// server exposing the domain read-model as JSON plus a static single-page
// UI. Domain-only frontend — it reaches git through internal/domain, never
// internal/git (archtest-guarded).
package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
)

// Server serves the probe's JSON API and static assets for one repository.
type Server struct {
	svc *domain.Service

	mu   sync.Mutex
	feed *domain.CommitFeed

	// page-size overrides applied to the feed when > 0 (test seam).
	pageInitial int
	pageBatch   int
}

func New(svc *domain.Service) *Server { return &Server{svc: svc} }

// Handler returns the full route mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/repo", s.handleRepo)
	return mux
}

func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	top, err := s.svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	branch, err := s.svc.CurrentBranch(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"name":     filepath.Base(top),
		"worktree": top,
		"branch":   branch,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
