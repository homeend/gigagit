package web

import "net/http"

// maxRemoteRows caps the sidebar payload (the tags cap precedent) — big
// monorepos carry thousands of remote-tracking branches.
const maxRemoteRows = 100

type remoteRow struct {
	Name   string `json:"name"`   // short ref, e.g. "origin/feature/x"
	Remote string `json:"remote"` // "origin"
	Branch string `json:"branch"` // "feature/x"
	Hash   string `json:"hash"`   // short object name
	Time   int64  `json:"time"`
}

func (s *Server) handleRemotes(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	rbs, err := svc.RemoteBranches(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	truncated := false
	if len(rbs) > maxRemoteRows {
		rbs = rbs[:maxRemoteRows]
		truncated = true
	}
	rows := make([]remoteRow, 0, len(rbs))
	for _, rb := range rbs {
		rows = append(rows, remoteRow{Name: rb.Name, Remote: rb.Remote, Branch: rb.Branch, Hash: rb.Hash, Time: rb.UnixTime})
	}
	writeJSON(w, map[string]any{"remotes": rows, "truncated": truncated})
}
