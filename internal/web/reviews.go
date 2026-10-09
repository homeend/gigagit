package web

import (
	"context"
	"errors"
	"fmt"
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
	// Remarks / Resolved: the review row's tally.
	Remarks  int `json:"remarks"`
	Resolved int `json:"resolved"`
	// Older marks a preview review of a tip its preview has since moved
	// past (domain classifies it; spec R2). Absent on every other review.
	Older bool `json:"older,omitempty"`
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
		out = append(out, reviewHeadWire{ID: h.ID, Commit: h.Commit, Branch: h.Branch, Agent: h.Agent, Summary: h.Summary, Created: wireTime(h.Created),
			Remarks: h.Remarks, Resolved: h.Resolved, Older: h.Older})
	}
	return out
}

// reviewHeadsOf is reviewHeads for full reviews.
func reviewHeadsOf(rs []domain.Review) []reviewHeadWire {
	out := make([]reviewHeadWire, 0, len(rs))
	for _, r := range rs {
		remarks, resolved := r.Tally()
		out = append(out, reviewHeadWire{ID: r.ID, Commit: r.Commit, Branch: r.Branch, Agent: r.Agent, Summary: r.Summary, Created: wireTime(r.Created),
			Remarks: remarks, Resolved: resolved})
	}
	return out
}

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/review/notes", s.handleReviewNotes)
		mux.HandleFunc("GET /api/review/{id}", s.handleReview)
		mux.HandleFunc("GET /api/review/{id}/link", s.handleReviewLink)
		mux.HandleFunc("GET /api/review/{id}/remark-id", s.handleReviewRemarkID)
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
	if rv.Kind == domain.ReviewOnWorktree {
		label = "working changes"
	}
	if rv.Preview != "" {
		// A preview's review is named by its preview (the TUI's header).
		label = domain.NoteScopeLabel(rv.Preview)
	}
	out := map[string]any{
		"id": rv.ID, "agent": rv.Agent, "created": wireTime(rv.Created), "branch": rv.Branch,
		"commit": rv.Commit, "label": label, "structured": rv.Doc != nil, "text": rv.Text,
		"base": "", "tip": rv.Commit, "range": false,
		// older: a preview review of a tip its preview has since moved past.
		"older": svc.ReviewOlder(ctx, rv),
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
	var states map[string]domain.WorkingFileState
	if rv.Kind == domain.ReviewOnWorktree {
		// A review of uncommitted changes: HEAD ↔ the working tree, each
		// reviewed file marked against the bytes the review read.
		states = domain.WorkingReviewState(rv.Worktree, rv.Files).States
		ws := make(map[string]string, len(states))
		for p, st := range states {
			ws[p] = workingStateWord(st)
		}
		out["working"], out["states"] = true, ws
	}
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
	out["overviewMd"] = markdown.Parse(rv.Doc.Summary)
	out["meta"] = reviewMetaText(rv.Doc.Meta)
	out["notes"], out["note_files"] = rv.Doc.NoteCount()
	_, out["resolved"] = rv.Tally()
	other := []map[string]string{}
	if os, err := svc.ReviewOtherNotes(ctx, rv.ID); err == nil {
		for _, o := range os {
			row := map[string]string{"path": o.Path, "line": reviewLine(o.Side, o.Range), "summary": o.Summary}
			if o.Changed {
				row["changed"] = "1"
			}
			if o.Outdated { // its remark is gone from the re-saved review: no path:line
				row = map[string]string{"summary": o.Summary, "outdated": "1"}
			}
			if len(o.Replies) > 0 {
				row["replies"] = strconv.Itoa(len(o.Replies))
			}
			if o.Resolution != nil {
				row["resolved"] = "1"
			}
			other = append(other, row)
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
	if rv.Kind == domain.ReviewOnWorktree {
		// The page shows HEAD → the working tree (/api/diff?wt=head).
		d, _, err := worktreeDiff(ctx, svc, "head", path, oldPath, nil, false)
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

// workingStateWord is a working review file's state on the wire.
func workingStateWord(st domain.WorkingFileState) string {
	switch st {
	case domain.WorkingFileMatches:
		return "matches"
	case domain.WorkingFileGone:
		return "gone"
	}
	return "changed"
}

// handleReviewRemarkID is remark ?n= of review {id}'s id (Copy remark id),
// handed back only while the review holds that remark: a copied id must be
// one gg note reply takes.
func (s *Server) handleReviewRemarkID(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil || n < 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad remark number %q", r.URL.Query().Get("n")))
		return
	}
	id, err := s.service().ReviewRemarkID(readCtx(r), model.ReviewNoteIDPrefix+r.PathValue("id")+":"+strconv.Itoa(n))
	switch {
	case errors.Is(err, domain.ErrReviewNotFound), errors.Is(err, domain.ErrNoSuchRemark):
		writeErr(w, http.StatusNotFound, err)
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, map[string]any{"id": id})
	}
}

// handleReviewLink is a review's gg link (Copy gg link on its rows): only the
// server knows the change the review compared, so the page never builds it.
func (s *Server) handleReviewLink(w http.ResponseWriter, r *http.Request) {
	svc, rv, ok := s.knownReview(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	// ?path= names a file of the review, ?n= one of its remarks: the link
	// then reopens the review there.
	q := r.URL.Query()
	path, n := q.Get("path"), q.Get("n")
	var link string
	var err error
	switch {
	case path != "" && n != "":
		writeErr(w, http.StatusBadRequest, errors.New("pass path or n, not both"))
		return
	case path != "":
		link, err = svc.ReviewFileLink(readCtx(r), rv.ID, path)
	case n != "":
		k, aerr := strconv.Atoi(n)
		if aerr != nil || k < 0 {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("bad remark number %q", n))
			return
		}
		if link, err = svc.ReviewRemarkLink(readCtx(r), model.ReviewNoteIDPrefix+rv.ID+":"+strconv.Itoa(k)); err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
	default:
		link, err = svc.ReviewLink(readCtx(r), rv.ID)
	}
	if errors.Is(err, domain.ErrNotInReview) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"link": link})
}
