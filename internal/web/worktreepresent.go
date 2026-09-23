package web

import (
	"errors"
	"net/http"
	"strings"
)

// handleWorktreePresent serves GET /api/worktree-present?path=<p>: is that
// file on disk in this worktree? The web's "copy file link" asks it before
// copying a content link (?view=content). A stat, no git
// (domain.WorktreeFilesPresent); an escaping path is simply absent.
func (s *Server) handleWorktreePresent(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeErr(w, http.StatusBadRequest, errors.New("path required"))
		return
	}
	got, err := s.service().WorktreeFilesPresent(r.Context(), []string{p})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"present": got[p]})
}
