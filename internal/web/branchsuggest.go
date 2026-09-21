package web

import (
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/fuzzy"
)

// branchSuggestLimit is the TUI preview form's completion strip size
// (previewSuggestLimit): five names fit one line.
const branchSuggestLimit = 5

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/branch-suggest", s.handleBranchSuggest)
	})
}

// handleBranchSuggest ranks the names a branch prompt may take — local
// branches, then remote-tracking ones — against ?q=, the TUI's
// branchSuggestions: the same candidates, the same fuzzy.Rank, the same
// "an empty field lists nothing". Ranked here, not in the page, so the two
// frontends order hints identically.
func (s *Server) handleBranchSuggest(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	names := []string{}
	if q == "" {
		writeJSON(w, map[string]any{"names": names})
		return
	}
	svc := s.service()
	ctx := readCtx(r)
	bs, err := svc.Branches(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cands := make([]string, 0, len(bs))
	for _, b := range bs {
		cands = append(cands, b.Name)
	}
	// A remote listing that fails only narrows the hints.
	if rbs, rerr := svc.RemoteBranches(ctx); rerr == nil {
		for _, rb := range rbs {
			cands = append(cands, rb.Name)
		}
	}
	for _, m := range fuzzy.Rank(q, cands, branchSuggestLimit) {
		names = append(names, m.S)
	}
	writeJSON(w, map[string]any{"names": names})
}
