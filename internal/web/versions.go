package web

import (
	"errors"
	"net/http"
)

// Branch versions — the operations history. Every destructive op snapshots
// the branch tip to refs/gg/versions/<branch>/<unix>-<op> first, and this is
// the read side of that: what the branch pointed at before each one.
//
// The branch name is NOT resolved against the live branch list, unlike solo
// or remove-worktree. There a bad value broke something (an unrenderable
// scope, a git argv); here BranchVersions is one for-each-ref under a
// prefix, so an unknown branch simply comes back empty and renders as "no
// versions" — and a DELETED branch's versions are exactly what this read is
// for. isGitArgSafe is the whole guard.

type versionRow struct {
	Ref     string `json:"ref"`
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Op      string `json:"op"` // protocol token: merge, rebase, amend, …
	Unix    int64  `json:"unix"`
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	branch := r.URL.Query().Get("branch")
	if !isGitArgSafe(branch) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
		return
	}
	vs, err := s.service().BranchVersions(r.Context(), branch)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]versionRow, 0, len(vs))
	for _, v := range vs {
		short := v.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		rows = append(rows, versionRow{
			Ref: v.Ref, Hash: v.Hash, Short: short,
			Subject: v.Subject, Op: v.Op, Unix: v.Unix,
		})
	}
	writeJSON(w, map[string]any{"branch": branch, "versions": rows})
}

// vbranchRow is one branch with recorded versions — the all-branches picker
// read, and the only route to a DELETED branch's snapshots (recorded by
// delete-branch itself; restore-version recreates the ref).
type vbranchRow struct {
	Branch     string `json:"branch"`
	Deleted    bool   `json:"deleted"`
	Count      int    `json:"count"`
	LatestUnix int64  `json:"latest_unix"`
}

func (s *Server) handleVersionBranches(w http.ResponseWriter, r *http.Request) {
	vs, err := s.service().AllVersionBranches(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]vbranchRow, 0, len(vs))
	for _, v := range vs {
		rows = append(rows, vbranchRow{
			Branch: v.Branch, Deleted: v.Deleted,
			Count: v.Count, LatestUnix: v.LatestUnix,
		})
	}
	writeJSON(w, map[string]any{"branches": rows})
}
