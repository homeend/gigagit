package web

import (
	"errors"
	"net/http"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/branch-sha", s.handleBranchSha)
	})
}

// handleBranchSha serves the branch menu's Copy commit sha: a branch's full
// commit id at the moment it is asked for. The wire's branch rows carry only
// git's abbreviated id. The full refname keeps a tag of the same name, which
// plain rev-parse prefers, from answering for the branch.
func (s *Server) handleBranchSha(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" || !isGitArgSafe(name) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
		return
	}
	sha, err := s.service().RevParse(r.Context(), "refs/heads/"+name)
	if err != nil || sha == "" {
		writeErr(w, http.StatusNotFound, errors.New("no such branch: "+name))
		return
	}
	writeJSON(w, map[string]string{"sha": sha})
}
