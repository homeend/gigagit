package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Review notes — the web half of phase 1.
//
// Notes are machine-local working material anchored to a line on one side of
// one file. The browser never names a checkout: `Address.Worktree` is filled
// server-side from the Service's own top level (domain.NotesAt / NoteAdd), so
// a page cannot address a sibling worktree's content, and every other wire
// value is resolved through an allowlist before it can reach a git argv.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/notes", s.handleNotes)
		mux.HandleFunc("GET /api/notes/counts", s.handleNoteCounts)
		mux.HandleFunc("POST /api/notes/add", writeGuard(s.handleNoteAdd))
		mux.HandleFunc("POST /api/notes/edit", writeGuard(s.handleNoteEdit))
		mux.HandleFunc("POST /api/notes/reply", writeGuard(s.handleNoteReply))
		mux.HandleFunc("POST /api/notes/remove", writeGuard(s.handleNoteRemove))
	})
}

// noteState is the allowlist of wire state values → model.FileState. Anything
// else is a 400: untrusted params reach git argv through the address.
func noteState(s string) (model.FileState, bool) {
	switch s {
	case "", "unstaged":
		return model.StateUnstaged, true
	case "staged":
		return model.StateStaged, true
	case "untracked":
		return model.StateUntracked, true
	case "commit":
		return model.StateCommitted, true
	}
	return 0, false
}

// noteSide is the allowlist of wire side values.
func noteSide(s string) (model.NoteSide, bool) {
	switch s {
	case "", "new":
		return model.NoteSideNew, true
	case "old":
		return model.NoteSideOld, true
	}
	return "", false
}

// noteAddress builds the address a note hangs off from wire values. Worktree
// is deliberately absent: domain fills it from the server's own checkout.
func noteAddress(path, rev, state string) (model.FileAddress, error) {
	st, ok := noteState(state)
	if !ok {
		return model.FileAddress{}, errors.New("state must be unstaged, staged, untracked or commit")
	}
	if path == "" {
		return model.FileAddress{}, errors.New("path is required")
	}
	if !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) {
		return model.FileAddress{}, errors.New("invalid path/rev")
	}
	if st == model.StateCommitted && rev == "" {
		return model.FileAddress{}, errors.New("a commit note needs a rev")
	}
	if st != model.StateCommitted {
		rev = "" // a working-tree note has no revision; never store a stray one
	}
	return model.FileAddress{State: st, Commit: rev, Path: path}, nil
}

type wireNote struct {
	ID        string     `json:"id"`
	ParentID  string     `json:"parent_id,omitempty"`
	Source    string     `json:"source"`
	Author    string     `json:"author,omitempty"`
	Side      string     `json:"side"`
	Line      int        `json:"line"`
	Summary   string     `json:"summary"`
	Rationale string     `json:"rationale,omitempty"`
	Status    string     `json:"status"`
	Replies   []wireNote `json:"replies,omitempty"`
}

// toWireNote flattens one resolved thread. Line is the RESOLVED anchor (the
// range's end), not the stored one: a note that moved must render where its
// text is now.
func toWireNote(r domain.ResolvedNote) wireNote {
	w := wireNote{
		ID: r.Note.ID, ParentID: r.Note.ParentID, Source: string(r.Note.Source),
		Author: r.Note.Author, Side: string(r.Note.Side), Line: r.Range[1],
		Summary: r.Note.Summary, Rationale: r.Note.Rationale, Status: string(r.Status),
	}
	for _, rep := range r.Replies {
		w.Replies = append(w.Replies, toWireNote(rep))
	}
	return w
}

func (s *Server) handleNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	addr, err := noteAddress(q.Get("path"), q.Get("rev"), q.Get("state"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.service().NotesAt(r.Context(), addr)
	if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]wireNote, 0, len(res))
	for _, n := range res {
		out = append(out, toWireNote(n))
	}
	writeJSON(w, map[string]any{"notes": out})
}

// handleNoteCounts serves the ◆N badges. Notes being off is not an error to a
// painter: it simply has no badges to draw.
func (s *Server) handleNoteCounts(w http.ResponseWriter, r *http.Request) {
	// The page asks for counts only on a deliberate refresh (a hello, a
	// `notes` live event, a mutation) — never on the poll ticker — so the read
	// bypasses the cache. Without that, the startup sweep's rewrite and any
	// write by another gg process stay invisible for the life of the server.
	s.service().InvalidateNoteCounts()
	c, err := s.service().NoteCounts(r.Context())
	if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"by_path":        orEmptyCounts(c.ByPath),
		"by_commit":      orEmptyCounts(c.ByCommit),
		"by_commit_path": orEmptyCounts(c.ByCommitPath),
	})
}

// orEmptyCounts keeps the three fields OBJECTS on the wire even when notes are
// off — a JSON null would make every client-side lookup a guarded one.
func orEmptyCounts(m map[string]int) map[string]int {
	if m == nil {
		return map[string]int{}
	}
	return m
}

// The store's budget is an entry COUNT, not a byte budget, so without these a
// single paste could put a megabyte of text into notes.toml — a note is a
// review remark, not a document. Loopback-only, so this is hygiene rather than
// a security boundary; the caller is told rather than silently truncated.
const (
	noteSummaryMax   = 2 << 10  // a one-line remark
	noteRationaleMax = 16 << 10 // the "why", a few paragraphs at most
	noteAuthorMax    = 256      // a name or a tool label
)

// checkNoteText refuses over-long free-text fields. Lengths are in BYTES: the
// point is the file's size, not a display width.
func checkNoteText(summary, rationale, author string) error {
	for _, f := range []struct {
		name string
		val  string
		max  int
	}{
		{"summary", summary, noteSummaryMax},
		{"rationale", rationale, noteRationaleMax},
		{"author", author, noteAuthorMax},
	} {
		if len(f.val) > f.max {
			return fmt.Errorf("%s is %d bytes; the limit is %d", f.name, len(f.val), f.max)
		}
	}
	return nil
}

type noteReq struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Rev       string `json:"rev"`
	State     string `json:"state"`
	Side      string `json:"side"`
	Line      int    `json:"line"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale"`
	Author    string `json:"author"`
}

func decodeNoteReq(w http.ResponseWriter, r *http.Request) (noteReq, bool) {
	var req noteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return noteReq{}, false
	}
	return req, true
}

// noteAuthor is the name a browser-written note carries: the request's own
// author when it sends one, else the repo's effective git user.name — the
// same source the TUI stamps, so a note reads "note · ada · file R12" from
// either frontend.
func (s *Server) noteAuthor(ctx context.Context, given string) string {
	if a := strings.TrimSpace(given); a != "" {
		return a
	}
	if id, err := s.service().Identity(ctx); err == nil {
		return strings.TrimSpace(id.EffectiveName)
	}
	return ""
}

func (s *Server) handleNoteAdd(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	addr, err := noteAddress(req.Path, req.Rev, req.State)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	side, sok := noteSide(req.Side)
	summary := strings.TrimSpace(req.Summary)
	if !sok || req.Line < 1 || summary == "" {
		writeErr(w, http.StatusBadRequest, errors.New("side, a 1-based line and a summary are required"))
		return
	}
	if err := checkNoteText(summary, req.Rationale, req.Author); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	n := model.Note{
		Source: model.NoteSourceUser, Author: s.noteAuthor(r.Context(), req.Author), Address: addr,
		Side: side, Range: [2]int{req.Line, req.Line},
		Summary: summary, Rationale: strings.TrimSpace(req.Rationale),
	}
	// ContextHash is left empty on purpose: domain fills it from the side text
	// (the browser has the rendered row, but the server is the authority here).
	got, err := s.service().NoteAdd(r.Context(), n)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"id": got.ID})
}

func (s *Server) handleNoteEdit(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	summary := strings.TrimSpace(req.Summary)
	if req.ID == "" || summary == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id and a summary are required"))
		return
	}
	if err := checkNoteText(summary, req.Rationale, req.Author); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.service().NoteEdit(r.Context(), req.ID, summary, strings.TrimSpace(req.Rationale)); err != nil {
		writeErr(w, noteErrStatus(err), err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleNoteReply(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	summary := strings.TrimSpace(req.Summary)
	if req.ID == "" || summary == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id and a summary are required"))
		return
	}
	if err := checkNoteText(summary, req.Rationale, req.Author); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// The reply's anchor is the PARENT's (domain copies address/side/range and
	// flattens to the thread root); nothing about it comes off the wire.
	got, err := s.service().NoteReply(r.Context(), req.ID, model.Note{
		Source: model.NoteSourceUser, Author: s.noteAuthor(r.Context(), req.Author),
		Summary: summary, Rationale: strings.TrimSpace(req.Rationale),
	})
	if err != nil {
		writeErr(w, noteErrStatus(err), err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"id": got.ID})
}

func (s *Server) handleNoteRemove(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	if req.ID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}
	if err := s.service().NoteRemove(r.Context(), req.ID); err != nil {
		writeErr(w, noteErrStatus(err), err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"ok": true})
}

// noteErrStatus separates "you named a note that is not there" (a stale page
// after a sweep or another client's delete) from a real store failure. It
// matches domain's own sentinel, which wraps the store's notes.ErrNotFound so
// a frontend never has to import internal/notes (archtest forbids it).
func noteErrStatus(err error) int {
	if errors.Is(err, domain.ErrNoteNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// emitNotes tells every open page that notes changed. "notes" is NOT in
// liveSources: the ticker never polls it, this is the only producer.
func (s *Server) emitNotes() {
	if h := s.liveHubRef(); h != nil {
		h.emit(liveMsg{Changed: []string{"notes"}, Reason: "notes"})
	}
}
