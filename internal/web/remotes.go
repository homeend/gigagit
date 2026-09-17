package web

import (
	"net/http"

	"github.com/homeend/gigagit/internal/branchfilter"
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
	// Exempt marks a row the active rule WOULD have hidden but may not: the
	// current branch's upstream. Unlike /api/branches this payload DROPS the
	// hidden rows, so exempt is the only verdict that reaches the wire.
	Exempt bool `json:"exempt"`
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
	var verdicts []branchfilter.Verdict
	hidden := 0
	if active != nil {
		bs, berr := svc.Branches(ctx)
		if berr != nil {
			bs = nil // no upstream row to protect; the rule simply applies to all
		}
		verdicts, hidden = applyFilter(active, domain.RemoteBranchRows(rbs), domain.ExemptRemoteBranches(rbs, bs))
	}
	// Hidden rows are dropped here (the branches payload flags them instead —
	// it has no cap to spend them on), so the row is built in the same pass
	// that reads its verdict: that is what keeps the exempt flag with the
	// row it belongs to once the indexes stop matching.
	rows := make([]remoteRow, 0, len(rbs))
	for i, rb := range rbs {
		if verdicts != nil && verdicts[i].Hidden {
			continue
		}
		row := remoteRow{Name: rb.Name, Remote: rb.Remote, Branch: rb.Branch, Hash: rb.Hash, Time: rb.UnixTime}
		if verdicts != nil {
			row.Exempt = verdicts[i].Exempt
		}
		rows = append(rows, row)
	}
	truncated := false
	if len(rows) > maxRemoteRows {
		rows = rows[:maxRemoteRows]
		truncated = true
	}
	writeJSON(w, map[string]any{"remotes": rows, "truncated": truncated, "filter": wireFor(active, hidden)})
}
