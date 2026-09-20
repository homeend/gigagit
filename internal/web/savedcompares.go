package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// The Previews tab's other two kinds. A merge preview keeps /api/preview, which
// carries its live summary, its notes and the pull-request lane; these routes
// serve what that one never shows:
//
//	a PAIR        one saved link holding @<a>..<b> — two frozen commits
//	a COMPARISON  two saved gg:// links
//
// "Pair" is never used for a two-link entry. The kinds are told apart by
// domain (PairGet / SavedCompare.IsSet), not re-derived from link text here.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/saved-compares", writeGuard(s.handleSavedCompareAdd))
	})
}

// maxSavedCompareBody bounds a POST body: two links and a label.
const maxSavedCompareBody = 8 << 10

type savedCompareRow struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // "pair" | "compare"
	// a pair
	Link  string `json:"link,omitempty"`
	A     string `json:"a,omitempty"`
	B     string `json:"b,omitempty"`
	State string `json:"state,omitempty"`
	Files int    `json:"files,omitempty"`
	Error string `json:"error,omitempty"`
	// a comparison
	Left      string `json:"left,omitempty"`
	Right     string `json:"right,omitempty"`
	LeftDesc  string `json:"left_desc,omitempty"`
	RightDesc string `json:"right_desc,omitempty"`
}

func comparisonRow(c domain.SavedCompare) savedCompareRow {
	return savedCompareRow{ID: c.ID, Label: c.Label, Kind: "compare", Left: c.Left, Right: c.Right}
}

func pairRow(p domain.CommitPair) savedCompareRow {
	return savedCompareRow{ID: p.ID, Label: p.Label, Kind: "pair", A: p.A, B: p.B}
}

// writeAlreadySaved answers a duplicate with the row that is already there,
// so the page can say which one (the preview add's 409 shape, plus the label).
func writeAlreadySaved(w http.ResponseWriter, id, label string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "already saved", "id": id, "label": label})
}

// handleSavedCompareAdd saves a comparison ({left, right, label}) or a pair
// ({a, b, label}). The label goes to domain UNTOUCHED: an empty one takes the
// store's default, a rule this package must not copy.
func (s *Server) handleSavedCompareAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ Left, Right, A, B, Label string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSavedCompareBody)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	links, pair := req.Left != "" || req.Right != "", req.A != "" || req.B != ""
	svc := s.service()
	ctx := readCtx(r)
	switch {
	case links == pair:
		writeErr(w, http.StatusBadRequest, errors.New("give left + right (a comparison) or a + b (a pair)"))
	case links:
		// BOTH halves: SavedCompareAdd stores a one-link SET for an empty
		// right, which is a merge preview's shape, not a comparison's.
		if req.Left == "" || req.Right == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a comparison needs both links"))
			return
		}
		c, err := svc.SavedCompareAdd(ctx, req.Left, req.Right, req.Label)
		if errors.Is(err, domain.ErrSavedCompareExists) {
			writeAlreadySaved(w, c.ID, c.Label)
			return
		}
		if err != nil {
			writeErr(w, savedCompareErrStatus(err, http.StatusUnprocessableEntity), err)
			return
		}
		s.emitPreviews()
		writeJSON(w, map[string]any{"entry": comparisonRow(c)})
	default:
		// Full ids only: a pair is two FROZEN commits, and this wire resolves
		// no names (the page sends what a saved row or a landing handed it).
		if !isFullSha(req.A) || !isFullSha(req.B) {
			writeErr(w, http.StatusBadRequest, errors.New("a and b must be full commit ids"))
			return
		}
		p, err := svc.PairAdd(ctx, req.A, req.B, req.Label)
		if errors.Is(err, domain.ErrPairExists) {
			writeAlreadySaved(w, p.ID, p.Label)
			return
		}
		if err != nil {
			writeErr(w, savedCompareErrStatus(err, http.StatusUnprocessableEntity), err)
			return
		}
		s.emitPreviews()
		writeJSON(w, map[string]any{"entry": pairRow(p)})
	}
}

// isFullSha: a FULL object id — 40 hex characters, or 64 in a sha-256 repo.
func isFullSha(s string) bool {
	return (len(s) == 40 || len(s) == 64) && isHexSha(s)
}

// savedCompareErrStatus separates "no such row" and "the store is off" from
// everything else, which answers fallback.
func savedCompareErrStatus(err error, fallback int) int {
	switch {
	case errors.Is(err, domain.ErrSavedCompareNotFound), errors.Is(err, domain.ErrPairNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrSavedComparesDisabled):
		return http.StatusServiceUnavailable
	}
	return fallback
}
