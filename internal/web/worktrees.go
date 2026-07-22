package web

import "net/http"

type worktreeRow struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Head     string `json:"head"`
	Detached bool   `json:"detached"`
	Bare     bool   `json:"bare"`
}

func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	ws, err := s.svc.Worktrees(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]worktreeRow, 0, len(ws))
	for _, wt := range ws {
		rows = append(rows, worktreeRow{Path: wt.Path, Branch: wt.Branch, Head: wt.Head, Detached: wt.Detached, Bare: wt.Bare})
	}
	writeJSON(w, map[string]any{"worktrees": rows})
}
