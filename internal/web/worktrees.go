package web

import "net/http"

type worktreeRow struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Head     string `json:"head"`
	Detached bool   `json:"detached"`
	Bare     bool   `json:"bare"`
	// Time is the HEAD commit's committer time — what "sort worktrees by
	// date" means, exactly as in the TUI (its headTimes map). 0 when unknown
	// (a bare worktree, or the lookup failed): those rows sort last.
	Time int64 `json:"time"`
}

func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ws, err := svc.Worktrees(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// One extra git invocation for every HEAD at once (the Snapshot query does
	// the same for the TUI). Best-effort: a failure costs the date sort its
	// keys, never the list.
	shas := make([]string, 0, len(ws))
	for _, wt := range ws {
		if wt.Head != "" {
			shas = append(shas, wt.Head)
		}
	}
	times := map[string]int64{}
	if len(shas) > 0 {
		if t, terr := svc.CommitTimes(readCtx(r), shas); terr == nil {
			times = t
		}
	}
	rows := make([]worktreeRow, 0, len(ws))
	for _, wt := range ws {
		rows = append(rows, worktreeRow{
			Path: wt.Path, Branch: wt.Branch, Head: wt.Head,
			Detached: wt.Detached, Bare: wt.Bare, Time: times[wt.Head],
		})
	}
	writeJSON(w, map[string]any{"worktrees": rows})
}
