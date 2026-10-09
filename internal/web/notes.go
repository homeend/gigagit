package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
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
		mux.HandleFunc("POST /api/notes/resolve", writeGuard(s.handleNoteResolve))
		mux.HandleFunc("POST /api/notes/remove", writeGuard(s.handleNoteRemove))
		mux.HandleFunc("POST /api/notes/clear-row", writeGuard(s.handleNoteClearRow))
		mux.HandleFunc("GET /api/notes/row-link", s.handleNoteRowLink)
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

// wireNote is domain's shared JSON note shape (see domain.WireNote): the CLI
// and MCP emit the same object, so a page and an agent read one format.
type wireNote = domain.WireNote

func toWireNote(r domain.ResolvedNote) wireNote { return domain.ToWireNote(r) }

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
	// A range review's notes belong to the review: a commit's own diff leaves
	// them out. View all notes opens a note where it is stored and asks for
	// every note at the address (scoped=1).
	if q.Get("scoped") != "1" {
		res = domain.PlainNotes(res)
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
		// …and the ones written outside any range: a commit's file badges.
		"plain_by_commit_path": orEmptyCounts(c.PlainByCommitPath),
		// The ranges each commit's notes were written in: its Range review rows.
		"scopes_by_commit": wireScopes(c.ScopesByCommit),
		"reviews":          reviewHeads(c.Reviews), // the Branches' review sub-rows
		// This worktree's working reviews, matched: the working list's Review
		// row and its ✎ markers.
		"working_reviews": s.workingReviewsWire(r, len(c.WorkingReviews) > 0),
		// The pull requests holding a local review (an AI review saved on
		// one, or notes written in its diff): the Pull requests list's ✎.
		"pr_reviewed": prReviewedWire(c),
	})
}

// prReviewedWire is NoteCounts.PRReviewed as a sorted array — never null.
func prReviewedWire(c domain.NoteCounts) []int {
	out := []int{}
	for n := range c.PRReviewed() {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
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
	ID    string `json:"id"`
	Path  string `json:"path"`
	Rev   string `json:"rev"`
	State string `json:"state"`
	Side  string `json:"side"`
	Line  int    `json:"line"`
	// First opens a note over a RANGE of lines (first..line, the note sits
	// under line); 0 = a one-line note.
	First     int    `json:"first"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale"`
	Author    string `json:"author"`
	// Preview is the scope the page wrote the note in ("<target>...<source>"
	// or "<a>..<b>"); stamped only when it resolves to the note's own commit.
	Preview string `json:"preview"`
	// PR is the pull request whose diff the page wrote the note in (0 =
	// none): the server names its scope — the page never sends a PR's names
	// back as refs (previews.js).
	PR int `json:"pr"`
	// Link: a reply's gg:// link or commit (domain validates it). Resolved:
	// the thread state /api/notes/resolve sets (required there).
	Link     string `json:"link"`
	Resolved *bool  `json:"resolved"`
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

// notePreview is the scope a page-written note records: the named preview or
// pair, resolved here (the wire value is never stored as sent), and only when
// its tip is the note's commit — the one place such a note is written. Any
// failure just leaves the note unstamped: the stamp is a label, not a gate.
func (s *Server) notePreview(ctx context.Context, spec string, addr model.FileAddress) string {
	if spec = strings.TrimSpace(spec); spec == "" || addr.State != model.StateCommitted {
		return ""
	}
	// A review this commit already holds (the page is inside it, opened from
	// the commit's Range review row): the note joins it under the same name,
	// however far its branch moved since. An allowlist — no resolving needed.
	if c, cerr := s.service().NoteCounts(ctx); cerr == nil {
		for _, sc := range c.ScopesByCommit[addr.Commit] {
			if domain.SameNoteScope(sc.Scope, spec) {
				return sc.Scope // the STORED name: a client's base half never passes
			}
		}
	}
	set, err := s.service().NoteScopeResolve(ctx, spec)
	if err != nil || set.Tip != addr.Commit {
		return ""
	}
	return set.Pair()
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
	first := req.First
	if first == 0 {
		first = req.Line
	}
	if first < 1 || first > req.Line {
		writeErr(w, http.StatusBadRequest, errors.New("a range's first line is 1-based and not past its last"))
		return
	}
	if err := checkNoteText(summary, req.Rationale, req.Author); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	preview := s.notePreview(r.Context(), req.Preview, addr)
	if req.PR > 0 && addr.State == model.StateCommitted {
		// The row the view was opened from (cachedPR): never a forge read.
		// A note that cannot take the PR's stamp is refused (ruling A): stored
		// plain it would silently leave the PR's view.
		pr, ok := s.cachedPR(s.service(), req.PR)
		if !ok {
			writeErr(w, http.StatusConflict, fmt.Errorf("pull request #%d is not in the pull request list any more — search for it and open it again", req.PR))
			return
		}
		sc, err := s.service().PRNoteScope(r.Context(), pr, addr.Commit)
		switch {
		case errors.Is(err, domain.ErrNoteOffPR), errors.Is(err, domain.ErrPRDiffGone):
			writeErr(w, http.StatusConflict, err)
			return
		case err != nil:
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		preview = sc
	}
	n := model.Note{
		Source: model.NoteSourceUser, Author: s.noteAuthor(r.Context(), req.Author), Address: addr,
		Side: side, Range: [2]int{first, req.Line},
		Summary: summary, Rationale: strings.TrimSpace(req.Rationale),
		Preview: preview,
	}
	// ContextHash is left empty on purpose: domain fills it from the side text
	// (the browser has the rendered row, but the server is the authority here).
	got, err := s.service().NoteAdd(r.Context(), n)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, domain.ErrNoteRange) {
			code = http.StatusBadRequest // lines the side does not hold: the request's mistake
		}
		writeErr(w, code, err)
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
		Summary: summary, Rationale: strings.TrimSpace(req.Rationale), Link: req.Link,
	})
	if err != nil {
		writeErr(w, noteErrStatus(err), err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"id": got.ID})
}

// handleNoteResolve resolves a thread (resolved:true) or reopens it, by any
// of its ids — a root, a reply or a review remark.
func (s *Server) handleNoteResolve(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeNoteReq(w, r)
	if !ok {
		return
	}
	if req.ID == "" || req.Resolved == nil {
		writeErr(w, http.StatusBadRequest, errors.New("id and resolved are required"))
		return
	}
	if _, err := s.service().NoteResolve(r.Context(), req.ID, *req.Resolved, s.noteAuthor(r.Context(), req.Author)); err != nil {
		writeErr(w, noteErrStatus(err), err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"ok": true})
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

// handleNoteClearRow deletes what one note row of a commit's file list stands
// for (the TUI's noteRowMenu Delete): {commit, path} = a Notes row's plain
// notes on path, {commit, scope} = a Range review row's notes. The commit is
// the full hex the commits feed hands the page; path and scope are only
// compared against stored notes, never read from disk.
func (s *Server) handleNoteClearRow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Commit string `json:"commit"`
		Path   string `json:"path"`
		Scope  string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !isHexSha(req.Commit) || (req.Path == "") == (req.Scope == "") {
		writeErr(w, http.StatusBadRequest, errors.New("a commit sha and one of path or scope are required"))
		return
	}
	n, err := s.service().NotesClearAtCommit(r.Context(), req.Commit, req.Path, req.Scope)
	if err != nil {
		writeErr(w, noteErrStatus(err), err)
		return
	}
	s.emitNotes()
	writeJSON(w, map[string]any{"removed": n})
}

// handleNoteRowLink is a commit's Range review / Notes row link (Copy gg
// link): ?commit=<sha>&path=<p> = the file at the commit, ?commit=<sha>&scope=
// <s> = the commit pair the scope names there. Built by domain, the one
// builder the TUI shares.
func (s *Server) handleNoteRowLink(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	commit, path, scope := q.Get("commit"), q.Get("path"), q.Get("scope")
	if !isHexSha(commit) || (path == "") == (scope == "") {
		writeErr(w, http.StatusBadRequest, errors.New("a commit sha and one of path or scope are required"))
		return
	}
	ctx, svc := readCtx(r), s.service()
	var link string
	var err error
	if scope != "" {
		link, err = svc.ScopeLinkText(ctx, scope, commit)
	} else {
		link, err = svc.CommitFileLinkText(ctx, commit, path)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]any{"link": link})
}

// noteErrStatus separates "you named a note that is not there" (a stale page
// after a sweep or another client's delete) from a real store failure. It
// matches domain's own sentinel, which wraps the store's notes.ErrNotFound so
// a frontend never has to import internal/notes (archtest forbids it).
func noteErrStatus(err error) int {
	switch {
	case errors.Is(err, domain.ErrNoteNotFound), errors.Is(err, domain.ErrReviewNotFound), errors.Is(err, domain.ErrNoSuchRemark):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrReadOnlyNote), errors.Is(err, domain.ErrForgeResolved), errors.Is(err, domain.ErrNotResolved):
		return http.StatusConflict
	case errors.Is(err, domain.ErrNoteLink):
		return http.StatusBadRequest
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

// workingReviewsWire is this worktree's working reviews, newest first, each
// with whether it is current and the files it still matches (sorted). Empty
// — never null — when there are none (some: the counts say so, no file read).
func (s *Server) workingReviewsWire(r *http.Request, some bool) []map[string]any {
	out := []map[string]any{}
	if !some {
		return out
	}
	rs, err := s.service().WorkingReviews(r.Context())
	if err != nil {
		return out
	}
	for _, wr := range rs {
		matches := []string{}
		for p, st := range wr.States {
			if st == domain.WorkingFileMatches {
				matches = append(matches, p)
			}
		}
		sort.Strings(matches)
		remarks, resolved := wr.Tally()
		out = append(out, map[string]any{"id": wr.ID, "summary": wr.Summary, "agent": wr.Agent,
			"created": wireTime(wr.Created), "current": wr.Current, "matches": matches,
			"remarks": remarks, "resolved": resolved})
	}
	return out
}
