package web

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/model"
)

// Stored AI reviews in the page: the TUI's review rows and review view
// (internal/tui/review_view.go). A review is one commit-level note; the page
// lists them from the note counts (a branch's sub-rows) and from an opened
// commit (its "Reviews" rows), and reads one whole for the review view.

// reviewHeadWire is one review as a list row needs it.
type reviewHeadWire struct {
	ID      string `json:"id"`
	Commit  string `json:"commit"`
	Branch  string `json:"branch,omitempty"`
	Agent   string `json:"agent"`
	Summary string `json:"summary"`
	Created string `json:"created"` // RFC3339 UTC, "" when unknown
}

func wireTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// reviewHeads is hs on the wire; never nil, so the field is always an array.
func reviewHeads(hs []domain.ReviewHead) []reviewHeadWire {
	out := make([]reviewHeadWire, 0, len(hs))
	for _, h := range hs {
		out = append(out, reviewHeadWire{ID: h.ID, Commit: h.Commit, Branch: h.Branch, Agent: h.Agent, Summary: h.Summary, Created: wireTime(h.Created)})
	}
	return out
}

// reviewHeadsOf is reviewHeads for full reviews.
func reviewHeadsOf(rs []domain.Review) []reviewHeadWire {
	out := make([]reviewHeadWire, 0, len(rs))
	for _, r := range rs {
		out = append(out, reviewHeadWire{ID: r.ID, Commit: r.Commit, Branch: r.Branch, Agent: r.Agent, Summary: r.Summary, Created: wireTime(r.Created)})
	}
	return out
}

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/review/notes", s.handleReviewNotes)
		mux.HandleFunc("GET /api/review/{id}", s.handleReview)
		mux.HandleFunc("GET /api/review/{id}/link", s.handleReviewLink)
	})
}

// knownReview reads review id, answering the refusal itself: 404 when it is
// gone (deleted in another client, the TUI, or swept). The id only ever
// matches a stored note's id — it never reaches a git argv.
func (s *Server) knownReview(w http.ResponseWriter, r *http.Request, id string) (*domain.Service, domain.Review, bool) {
	svc := s.service()
	rv, err := svc.Review(readCtx(r), id)
	switch {
	case errors.Is(err, domain.ErrReviewNotFound):
		writeErr(w, http.StatusNotFound, err)
		return nil, rv, false
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err)
		return nil, rv, false
	}
	return svc, rv, true
}

// reviewLine is a document note's line as the CLI prints it: "2-3", and a
// leading "-" for an old-side line.
func reviewLine(side model.NoteSide, rng [2]int) string {
	line := strconv.Itoa(rng[0])
	if rng[1] != rng[0] {
		line += "-" + strconv.Itoa(rng[1])
	}
	if side == model.NoteSideOld {
		line = "-" + line
	}
	return line
}

// handleReview is the review view's one load: the review, the files it
// lists (the commit's, or a range review's), per-file note counts and
// summaries, the overview as a markdown tree and the notes it cannot place.
func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	svc, rv, ok := s.knownReview(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	ctx := readCtx(r)
	label := shortSha(rv.Commit)
	if rv.Branch != "" {
		label = rv.Branch + " " + label
	}
	out := map[string]any{
		"id": rv.ID, "agent": rv.Agent, "created": wireTime(rv.Created), "branch": rv.Branch,
		"commit": rv.Commit, "label": label, "structured": rv.Doc != nil, "text": rv.Text,
		"base": "", "tip": rv.Commit, "range": false,
		"files": []map[string]string{}, "counts": map[string]int{}, "summaries": map[string]string{},
		"meta": "", "notes": 0, "note_files": 0, "other": []map[string]string{},
	}
	if rv.Doc == nil {
		out["overviewMd"] = markdown.Parse(rv.Text) // a prose review: its text is the overview
		writeJSON(w, out)
		return
	}
	base, tip, isRange := svc.ReviewRevs(ctx, rv)
	out["base"], out["tip"], out["range"] = base, tip, isRange
	files, err := svc.ReviewFiles(ctx, rv)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	wf := make([]map[string]string, 0, len(files))
	for _, f := range files {
		row := map[string]string{"path": f.Path, "status": f.Status}
		if f.OldPath != "" {
			row["old_path"] = f.OldPath
		}
		wf = append(wf, row)
	}
	out["files"] = wf
	if c, err := svc.ReviewFileCounts(ctx, rv.ID); err == nil && c != nil {
		out["counts"] = c
	}
	sums := map[string]string{}
	for _, f := range rv.Doc.Files {
		if t := strings.TrimSpace(f.Summary); t != "" {
			sums[strings.TrimPrefix(filepath.ToSlash(f.Path), "./")] = t
		}
	}
	out["summaries"] = sums
	out["overviewMd"] = markdown.Parse(rv.Doc.Overview)
	out["meta"] = reviewMetaText(rv.Doc.Meta)
	out["notes"], out["note_files"] = rv.Doc.NoteCount()
	other := []map[string]string{}
	if os, err := svc.ReviewOtherNotes(ctx, rv.ID); err == nil {
		for _, o := range os {
			other = append(other, map[string]string{"path": o.Path, "line": reviewLine(o.Side, o.Range), "summary": o.Summary})
		}
	}
	out["other"] = other
	writeJSON(w, out)
}

// handleReviewNotes is the review view's note lane for one file: ONLY the
// review's notes (read-only, built from its document), resolved against the
// diff the page shows — the same Differ request /api/diff makes for the
// review's revisions, so after the page's diff fetch this is a cache hit.
func (s *Server) handleReviewNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path, oldPath, status := q.Get("path"), q.Get("old"), q.Get("status")
	if oldPath == "" {
		oldPath = path
	}
	if path == "" || !isGitArgSafe(path) || !isGitArgSafe(oldPath) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	svc, rv, ok := s.knownReview(w, r, q.Get("id"))
	if !ok {
		return
	}
	ctx := readCtx(r)
	notes := []wireNote{}
	if rv.Doc == nil {
		writeJSON(w, map[string]any{"notes": notes})
		return
	}
	base, tip, isRange := svc.ReviewRevs(ctx, rv)
	key := tip + "^.." + tip + ":" + path // /api/diff's commit form
	if isRange {
		key = base + ".." + tip + ":" + path // …and its rev form
	}
	var oldSrc, newSrc domain.ByteSource
	if status != "A" {
		oldSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, base, oldPath) }
	}
	if status != "D" {
		newSrc = func(ctx context.Context) ([]byte, error) { return svc.ShowFile(ctx, tip, path) }
	}
	d, err := svc.Differ().Diff(ctx, domain.Request{Key: key, Path: path, OldPath: oldPathFor(oldPath, path), Old: oldSrc, New: newSrc})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	res, err := svc.ReviewNotesFor(ctx, rv.ID, path, d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, n := range res {
		notes = append(notes, toWireNote(n))
	}
	writeJSON(w, map[string]any{"notes": notes})
}

// handleReviewLink is a review's gg link (Copy gg link on its rows): only the
// server knows the change the review compared, so the page never builds it.
func (s *Server) handleReviewLink(w http.ResponseWriter, r *http.Request) {
	svc, rv, ok := s.knownReview(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	link, err := svc.ReviewLink(readCtx(r), rv.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"link": link})
}
