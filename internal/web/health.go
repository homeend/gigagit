package web

import (
	"net/http"
)

// bigRepoPackBytes mirrors the TUI's floor (internal/tui/notify.go): below
// it the commit-graph win doesn't matter enough to suggest anything.
const bigRepoPackBytes = 100 << 20

// noticeCommitGraph is the TUI's commit-graph notice id — shared on purpose:
// dismissing the recommendation in either frontend silences both (same
// advice, same repo).
const noticeCommitGraph = "commit_graph_recommend"

// noticeWebGraphOff is the web-only graph-off suggestion id; the TUI has no
// equivalent auto-notice (its coupling fires on the Settings toggle).
const noticeWebGraphOff = "web_graph_off_suggest"

type healthResp struct {
	Big                 bool            `json:"big"`
	PackMB              int64           `json:"pack_mb"`
	HasCommitGraph      bool            `json:"has_commit_graph"`
	WriteCommitGraphSet bool            `json:"write_commit_graph_set"`
	ShowGraph           string          `json:"show_graph"`
	CommitSort          string          `json:"commit_sort"`
	Dismissed           map[string]bool `json:"dismissed"`
}

// packFloor is the effective big-repo threshold (test seam over the const).
func (s *Server) packFloor() int64 {
	if s.packThreshold > 0 {
		return s.packThreshold
	}
	return bigRepoPackBytes
}

// handleHealth projects domain.RepoHealth plus the effective UI settings and
// the banner ids' dismissal state. Read-only; config/promptstate failures
// degrade to defaults rather than erroring (the TUI's "health never surfaces
// errors" posture applies to the enrichments — only the core health read can
// fail the request).
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	h, err := svc.RepoHealth(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resp := healthResp{
		Big:                 h.PackBytes >= s.packFloor(),
		PackMB:              h.PackBytes / (1 << 20),
		HasCommitGraph:      h.HasCommitGraph,
		WriteCommitGraphSet: h.WriteCommitGraphSet,
		ShowGraph:           "on",
		CommitSort:          "date-order",
		Dismissed:           map[string]bool{noticeCommitGraph: false, noticeWebGraphOff: false},
	}
	if cfg, cerr := s.effectiveConfig(r.Context(), svc); cerr == nil {
		if cfg.UI.ShowGraph != "" {
			resp.ShowGraph = cfg.UI.ShowGraph
		}
		if cfg.UI.CommitSort != "" {
			resp.CommitSort = cfg.UI.CommitSort
		}
	}
	if store := s.promptStore(); store != nil && h.GitCommonDir != "" {
		d := store.DismissedNotices(h.GitCommonDir)
		resp.Dismissed[noticeCommitGraph] = d[noticeCommitGraph]
		resp.Dismissed[noticeWebGraphOff] = d[noticeWebGraphOff]
	}
	writeJSON(w, resp)
}
