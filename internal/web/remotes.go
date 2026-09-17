package web

import (
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
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
	ctx := readCtx(r)
	rbs, err := svc.RemoteBranches(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// sort → filter → cap. Sorting BEFORE the cap matters because sorting the
	// truncated window would show "the server's arbitrary first hundred,
	// sorted" — the wrong rows, not just the wrong order. Filtering before it
	// matters for the same reason: with a filter on, the cap must fall on the
	// hundred rows the user can actually see, or a rule that hides the newest
	// hundred would leave the section empty.
	rbs = sortedRows(rbs, allowedSortMode(r.URL.Query().Get("sort")),
		func(rb model.RemoteBranch) string { return rb.Name },
		func(rb model.RemoteBranch) int64 { return rb.UnixTime })
	// The slot is resolved first so a repo with no filter never pays for the
	// branch listing the upstream exemption needs.
	active := s.activeBranchFilter(ctx, svc, promptstate.BranchFilterListRemotes)
	hidden := 0
	if active != nil {
		bs, berr := svc.Branches(ctx)
		if berr != nil {
			bs = nil // no upstream row to protect; the rule simply applies to all
		}
		verdicts, n := applyFilter(active, domain.RemoteBranchRows(rbs), domain.ExemptRemoteBranches(rbs, bs))
		hidden = n
		kept := make([]model.RemoteBranch, 0, len(rbs)-hidden)
		for i, rb := range rbs {
			if verdicts[i].Hidden {
				continue
			}
			kept = append(kept, rb)
		}
		rbs = kept
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
	writeJSON(w, map[string]any{"remotes": rows, "truncated": truncated, "filter": wireFor(active, hidden)})
}
