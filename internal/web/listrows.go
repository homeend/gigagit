package web

// Small list-row actions the TUI has and the web lacked: forgetting a
// repository in the switcher (the TUI's R ctrl+d) and the Tags panel's
// one-shot "Refresh remote status".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/repos"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/repos/forget", writeGuard(s.handleReposForget))
		mux.HandleFunc("POST /api/remote-tags/refresh", writeGuard(s.handleRemoteTagsRefresh))
	})
}

// handleReposForget drops one entry from the MRU registry. The path must be
// one the registry holds (the switcher's own row), never an arbitrary one.
func (s *Server) handleReposForget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	path := s.reposStatePath()
	for _, e := range repos.Load(path) {
		if e.Path == req.Path {
			if err := repos.Remove(path, e.Path); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, map[string]any{})
			return
		}
	}
	writeErr(w, http.StatusNotFound, errors.New("that repository is not in the list"))
}

// handleRemoteTagsRefresh is the TUI's "Refresh remote status": one listing
// of the remote's tags now, its error reported (no remote, offline), its
// answer stored for the ▲ markers and announced to every tab.
func (s *Server) handleRemoteTagsRefresh(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx, cancel := context.WithTimeout(r.Context(), remoteTagListBudget)
	defer cancel()
	names, err := svc.RemoteTagsFresh(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	s.storeRemoteTags(svc, names)
	if h := s.liveHubRef(); h != nil {
		h.emit(liveMsg{Changed: []string{"tags"}, Reason: "remote-tags"})
	}
	writeJSON(w, map[string]any{"remote": len(names)})
}
