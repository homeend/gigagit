package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// Notes inside a merge preview. The page never names a checkout or a sha: it
// passes the pair's branch NAMES, which are resolved against the live branch
// lists (the /api/compare allowlist posture) before anything reaches a git
// argv, and the server answers with the resolved tip so the page's note-ADD
// calls can target it through the ordinary /api/notes/add endpoint.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/preview/notes", s.handlePreviewNotes)
	})
}

func (s *Server) handlePreviewNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	source, target, path := q.Get("source"), q.Get("target"), q.Get("path")
	for _, name := range []string{source, target} {
		if code, err := s.knownRefName(r, name); code != 0 {
			writeErr(w, code, err)
			return
		}
	}
	if path != "" && !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	ctx := readCtx(r)
	set, err := s.service().PreviewNotes(ctx, source, target)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Ruling 6: a pair that is not previewable is not an error — it simply
	// has nothing to show.
	out := map[string]any{"notes": []wireNote{}, "tip": set.Tip, "counts": map[string]int{}, "total": 0}
	if !set.OK() {
		writeJSON(w, out)
		return
	}
	// path == "" is the counts-only form — the preview-open badge fetch
	// (previews.js) and the file list's own refresh both call it with no
	// path to learn counts/total alone. PreviewNotesAt refuses an empty path
	// (errPreviewNotesNeedPath: resolving every file's notes against one
	// file's content is nobody's contract), so the counts-only form skips the
	// call rather than asking for notes it does not want.
	notes := []wireNote{}
	if path != "" {
		res, err := s.service().PreviewNotesAt(ctx, set, path)
		if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		notes = make([]wireNote, 0, len(res))
		for _, n := range res {
			notes = append(notes, domain.ToWireNotePreview(n, true))
		}
	}
	counts, total, cerr := s.service().PreviewNoteCounts(ctx, set)
	if cerr != nil && !errors.Is(cerr, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, cerr)
		return
	}
	out["notes"], out["counts"], out["total"] = notes, orEmptyCounts(counts), total
	writeJSON(w, out)
}
