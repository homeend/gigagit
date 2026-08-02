package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
)

// handleUIConfig writes [ui] show_graph / commit_sort to the COMMITTED repo
// .gg.toml (the feedFor probe's file) — the web's accept path for the
// big-repo banner's "graph off + plain sort". Values are allowlisted to the
// exact enum vocabulary: free config text never crosses the wire (the
// commit-edit "wire carries a verb" rule). Not an engine op — no git, no
// repogate; the same standing as the TUI Settings rows.
func (s *Server) handleUIConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShowGraph  string `json:"show_graph"`
		CommitSort string `json:"commit_sort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	if req.ShowGraph == "" && req.CommitSort == "" {
		writeErr(w, http.StatusBadRequest, errors.New("nothing to set"))
		return
	}
	if req.ShowGraph != "" && req.ShowGraph != "on" && req.ShowGraph != "off" {
		writeErr(w, http.StatusBadRequest, errors.New("invalid show_graph"))
		return
	}
	if req.CommitSort != "" && req.CommitSort != "date-order" && req.CommitSort != "plain" {
		writeErr(w, http.StatusBadRequest, errors.New("invalid commit_sort"))
		return
	}
	svc := s.service()
	top, err := svc.TopLevel(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	path := filepath.Join(top, ".gg.toml")
	if req.ShowGraph != "" {
		if err := config.SetShowGraph(path, req.ShowGraph); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	if req.CommitSort != "" {
		if err := config.SetCommitSort(path, req.CommitSort); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// feedFor re-reads commit_sort at the next build, so dropping the
		// feed is what makes the new sort take effect.
		s.resetFeed()
	}
	writeJSON(w, map[string]bool{"ok": true})
}
