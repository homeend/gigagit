package web

import (
	"net/http"

	"github.com/homeend/gigagit/internal/repos"
)

// handleRepos lists the machine's MRU registry (previously-opened repos) —
// the allowlist source a re-root picker chooses from.
func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	entries := repos.Load(s.reposStatePath())
	rows := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, map[string]string{"path": e.Path, "name": repos.Name(e)})
	}
	writeJSON(w, map[string]any{"repos": rows})
}
