package web

import (
	"net/http"

	"github.com/homeend/gigagit/internal/model"
)

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
	// Sort BEFORE the cap: sorting the truncated window would show "the
	// server's arbitrary first hundred, sorted" — the wrong rows, not just the
	// wrong order. Under date-desc this is what makes the section mean "the 100
	// most recently updated remote branches".
	rbs = sortedRows(rbs, allowedSortMode(r.URL.Query().Get("sort")),
		func(rb model.RemoteBranch) string { return rb.Name },
		func(rb model.RemoteBranch) int64 { return rb.UnixTime })
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
