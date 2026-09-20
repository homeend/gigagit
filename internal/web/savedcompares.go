package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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
		mux.HandleFunc("GET /api/saved-compares", s.handleSavedCompares)
		mux.HandleFunc("POST /api/saved-compares", writeGuard(s.handleSavedCompareAdd))
		mux.HandleFunc("POST /api/saved-compares/rename", writeGuard(s.handleSavedCompareRename))
		mux.HandleFunc("DELETE /api/saved-compares", writeGuard(s.handleSavedCompareRemove))
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
	Desc  string `json:"desc,omitempty"` // what copying the pair's link records
	A     string `json:"a,omitempty"`
	B     string `json:"b,omitempty"`
	State string `json:"state,omitempty"`
	Files int    `json:"files,omitempty"`
	// Notes is the pair's root-note total along a..b, the ◆N the TUI paints
	// on the row (the merge preview row's field, same meaning).
	Notes int    `json:"notes,omitempty"`
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

// savedEntry finds the row an id names and says which kind it is. It is the
// ONE classifier behind open, rename and remove: a merge preview's id — which
// lives in the same store — is "not found" here, so a surface that never shows
// previews cannot rename or delete one. The id must match EXACTLY: domain's
// getters also accept a label, and a label typed as an id must not reach
// another row.
func savedEntry(r *http.Request, svc *domain.Service, id string) (kind string, p domain.CommitPair, c domain.SavedCompare, err error) {
	if id == "" {
		return "", p, c, domain.ErrSavedCompareNotFound
	}
	ctx := r.Context()
	if p, err = svc.PairGet(ctx, id); err == nil && p.ID == id {
		return "pair", p, c, nil
	} else if err != nil && !errors.Is(err, domain.ErrPairNotFound) {
		return "", p, c, err
	}
	c, err = svc.SavedCompareGet(ctx, id)
	if err != nil {
		return "", p, c, err
	}
	if c.ID != id || c.IsSet() {
		return "", p, c, domain.ErrSavedCompareNotFound
	}
	return "compare", p, c, nil
}

// describeLinkText is a link text's one-line description, or the text itself
// when it no longer parses (the row must still list, so it can be removed).
func describeLinkText(r *http.Request, svc *domain.Service, text string) string {
	l, err := model.ParseLink(text)
	if err != nil {
		return text
	}
	return svc.DescribeLink(r.Context(), l)
}

// pairStateWire is the wire spelling of a pair's row state. No default arm
// that answers "ok": an unknown state must not read as openable.
func pairStateWire(st domain.PairState) string {
	switch st {
	case domain.PairOK:
		return "ok"
	case domain.PairMissingA:
		return "missing-a"
	case domain.PairMissingB:
		return "missing-b"
	case domain.PairInvalid:
		return "error"
	}
	return "error"
}

// handleSavedCompares lists the pairs, then the comparisons, each in store
// order. A comparison row carries NO live summary: evaluating two arbitrary
// links per row per refresh is unbounded work (a pair's count is cached by
// domain, and frozen).
func (s *Server) handleSavedCompares(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)
	all, err := svc.SavedCompareList(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrSavedComparesDisabled) {
			writeJSON(w, map[string]any{"entries": []savedCompareRow{}, "disabled": true})
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	pairs, err := svc.PairList(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	linkOf := make(map[string]string, len(all))
	for _, c := range all {
		linkOf[c.ID] = c.Left
	}
	rows := make([]savedCompareRow, 0, len(all))
	for _, p := range pairs {
		row := pairRow(p)
		row.Link = withPreviewHint(linkOf[p.ID], p.ID)
		row.Desc = describeLinkText(r, svc, row.Link)
		// One pair's transient git failure must not blank the list.
		if sum, err := svc.PairSummary(ctx, p.A, p.B); err != nil {
			row.State, row.Error = "error", err.Error()
		} else {
			row.State, row.Files = pairStateWire(sum.State), sum.Files
			// A pair with a missing half has no note scope at all: no
			// badge, and no store read (the merge rows' ruling 6).
			if sum.State == domain.PairOK {
				if set, serr := svc.PairNotes(ctx, p.A, p.B); serr == nil {
					if _, total, cerr := svc.PreviewNoteCounts(ctx, set); cerr == nil {
						row.Notes = total
					}
				}
			}
		}
		rows = append(rows, row)
	}
	for _, c := range all {
		if c.IsSet() {
			continue // a merge preview (/api/preview) or a pair (above)
		}
		row := comparisonRow(c)
		row.LeftDesc, row.RightDesc = describeLinkText(r, svc, c.Left), describeLinkText(r, svc, c.Right)
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"entries": rows})
}

func (s *Server) handleSavedCompareRename(w http.ResponseWriter, r *http.Request) {
	var req struct{ ID, Label string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSavedCompareBody)).Decode(&req); err != nil || req.ID == "" || req.Label == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id and label required"))
		return
	}
	svc := s.service()
	kind, _, _, err := savedEntry(r, svc, req.ID)
	if err == nil {
		if kind == "pair" {
			err = svc.PairRename(readCtx(r), req.ID, req.Label)
		} else {
			err = svc.SavedCompareRename(readCtx(r), req.ID, req.Label)
		}
	}
	if err != nil {
		writeErr(w, savedCompareErrStatus(err, http.StatusInternalServerError), err)
		return
	}
	s.emitPreviews()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSavedCompareRemove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	svc := s.service()
	kind, _, _, err := savedEntry(r, svc, id)
	if err == nil {
		if kind == "pair" {
			err = svc.PairRemove(readCtx(r), id)
		} else {
			err = svc.SavedCompareRemove(readCtx(r), id)
		}
	}
	if err != nil {
		writeErr(w, savedCompareErrStatus(err, http.StatusInternalServerError), err)
		return
	}
	s.emitPreviews()
	writeJSON(w, map[string]any{"ok": true})
}

// withPreviewHint is a saved set's link as its ROW copies it: the stored text
// plus ?preview=<id>, the landing that reveals the row again. The store keeps
// the bare text; an id or text the grammar cannot carry degrades to it.
func withPreviewHint(text, id string) string {
	l, err := model.ParseLink(text)
	if err != nil || !model.LinkHintIDOK(id) {
		return text
	}
	l.Hint = model.LinkHint{Kind: "preview", ID: id}
	return l.String()
}
