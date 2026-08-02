package web

import "net/http"

type stashRow struct {
	Ref          string `json:"ref"`
	Subject      string `json:"subject"`
	Sha          string `json:"sha,omitempty"`
	UntrackedSha string `json:"untracked_sha,omitempty"`
}

// handleStashes lists stash entries newest-first. Each row carries the
// stash's commit sha, resolved here so the client's left-click needs no
// second request (the tags-row pattern); a resolve failure drops only the
// sha, never the row.
func (s *Server) handleStashes(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	es, err := svc.StashList(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]stashRow, 0, len(es))
	for _, e := range es {
		row := stashRow{Ref: e.Ref, Subject: e.Subject}
		if sha, serr := svc.StashCommit(readCtx(r), e.Ref); serr == nil {
			row.Sha = sha
		}
		// A -u stash stores untracked files in a THIRD parent (a root
		// commit) invisible to the stash commit's first-parent diff;
		// surface it so the client can list and diff those files. The
		// input is the server-owned ref plus a literal — nothing
		// client-sent. No ^3 → rev-parse errors → field omitted.
		if usha, uerr := svc.StashCommit(readCtx(r), e.Ref+"^3"); uerr == nil {
			row.UntrackedSha = usha
		}
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"stashes": rows})
}
