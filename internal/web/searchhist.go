package web

// Search recall (the TUI's alt+↑/↓ in a filter or a search field): the
// per-repo rings domain keeps, read and recorded from the page. Only the
// scopes the page searches in; a phrase is recorded when it is confirmed.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/search-history", s.handleSearchHistory)
		mux.HandleFunc("POST /api/search-history", writeGuard(s.handleSearchRecord))
	})
}

// webSearchScopes are the rings the page reads and writes: the commit
// filter / @ highlight (panel), the in-view search (inview) and the file
// finder (filetree) — the TUI's scope names, so a phrase is shared.
var webSearchScopes = map[string]bool{"panel": true, "inview": true, "filetree": true}

func (s *Server) handleSearchHistory(w http.ResponseWriter, r *http.Request) {
	all := s.service().SearchHistoryAll(r.Context())
	rings := map[string][]string{}
	for scope := range webSearchScopes {
		rings[scope] = append([]string{}, all[scope]...)
	}
	writeJSON(w, map[string]any{"rings": rings})
}

func (s *Server) handleSearchRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope  string `json:"scope"`
		Phrase string `json:"phrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	if !webSearchScopes[req.Scope] {
		writeErr(w, http.StatusBadRequest, errors.New("unknown search scope"))
		return
	}
	phrase := strings.TrimSpace(req.Phrase)
	if phrase == "" {
		writeJSON(w, map[string]any{})
		return
	}
	svc := s.service()
	size := 0
	if cfg, err := s.effectiveConfig(r.Context(), svc); err == nil {
		size = cfg.UI.SearchHistorySize
	}
	svc.RecordSearch(r.Context(), req.Scope, phrase, size)
	writeJSON(w, map[string]any{})
}
