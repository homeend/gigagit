package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// Notes on a commit pair. The twin of /api/preview/notes for a scope that has
// no branch names to allowlist: the page passes the pair's two FULL commit ids
// (the ones /api/compare-links handed it), and the answer has the same shape —
// the gathered notes of one file, the write target, and every file's total.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/pair/notes", s.handlePairNotes)
	})
}

func (s *Server) handlePairNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a, b, path := q.Get("a"), q.Get("b"), q.Get("path")
	if !isFullSha(a) || !isFullSha(b) {
		writeErr(w, http.StatusBadRequest, errors.New("a and b must be full commit ids"))
		return
	}
	if path != "" && !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	ctx := readCtx(r)
	svc := s.service()
	set, err := svc.PairNotes(ctx, a, b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Ruling 6: a pair that is not here has nothing to show — not an error.
	out := map[string]any{"notes": []wireNote{}, "tip": set.Tip, "counts": map[string]int{}, "total": 0}
	if !set.OK() {
		writeJSON(w, out)
		return
	}
	// path == "" is the counts-only form (the file list's badges).
	notes := []wireNote{}
	if path != "" {
		res, err := svc.PreviewNotesAt(ctx, set, path)
		if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		notes = make([]wireNote, 0, len(res))
		for _, n := range res {
			notes = append(notes, domain.ToWireNoteRendered(n, true))
		}
	}
	counts, total, cerr := svc.PreviewNoteCounts(ctx, set)
	if cerr != nil && !errors.Is(cerr, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, cerr)
		return
	}
	out["notes"], out["counts"], out["total"] = notes, orEmptyCounts(counts), total
	writeJSON(w, out)
}
