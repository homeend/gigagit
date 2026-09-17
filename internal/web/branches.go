package web

import (
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

type branchRow struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	IsHead   bool   `json:"is_head"`
	Hash     string `json:"hash"`
	Time     int64  `json:"time"`
	// Hidden/Exempt are the active branch filter's verdict. Hidden rows stay
	// ON the wire (flagged) so the client can fold them behind the chip's
	// "show hidden" without a second request; Exempt marks a row the rule
	// would have hidden but may not (HEAD, or checked out in a worktree).
	Hidden bool `json:"hidden"`
	Exempt bool `json:"exempt"`
}

func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)
	bs, err := svc.Branches(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Resolve the slot BEFORE reaching for the exemption inputs: every live
	// refresh hits this route, and a repo with no filter on must not pay for
	// a worktree listing.
	active := s.activeBranchFilter(ctx, svc, promptstate.BranchFilterListBranches)
	var exempt []bool
	if active != nil {
		wts, werr := svc.Worktrees(ctx)
		if werr != nil {
			wts = nil // HEAD stays exempt either way; only the other checkouts are lost
		}
		exempt = domain.ExemptBranches(bs, wts)
	}
	verdicts, hidden := applyFilter(active, domain.BranchRows(bs), exempt)
	rows := make([]branchRow, 0, len(bs))
	for i, b := range bs {
		row := branchRow{
			Name: b.Name, Upstream: b.Upstream, Ahead: b.Ahead, Behind: b.Behind,
			IsHead: b.IsHead, Hash: b.Hash, Time: b.UnixTime,
		}
		if verdicts != nil {
			row.Hidden, row.Exempt = verdicts[i].Hidden, verdicts[i].Exempt
		}
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"branches": rows, "filter": wireFor(active, hidden)})
}
