package web

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/termimg"
)

// The viewer's read (open files, plan 5a): one file's lines at one version —
// the working tree (the bytes ON DISK), a commit, or a shelf entry — with the
// syntax runs /api/blame ships, so renderCell paints both alike.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/file-content", s.handleFileContent)
		mux.HandleFunc("GET /api/file-stamp", s.handleFileStamp)
		mux.HandleFunc("GET /api/file-raw", s.handleFileRaw)
	})
}

// handleFileRaw serves one IMAGE file's bytes at one version (the same
// src/rev/path as /api/file-content) for an <img>: the content type comes
// from the probed format, never the name. Anything that is no image is 415 —
// the page has no use for other raw bytes, so none are served.
func (s *Server) handleFileRaw(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path, src, rev := q.Get("path"), q.Get("src"), q.Get("rev")
	if path == "" || !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) || !filepath.IsLocal(filepath.FromSlash(path)) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path/rev"))
		return
	}
	switch src {
	case "", "worktree":
		src = "worktree"
	case "commit", "shelf":
		if rev == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a "+src+" version needs rev"))
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown src "+src))
		return
	}
	data, err := readVersion(readCtx(r), s.service(), src, rev, path)
	if errors.Is(err, fs.ErrNotExist) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(data) > domain.MaxDiffBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, errors.New("file too large"))
		return
	}
	kind, _, _, err := termimg.Probe(data)
	if err != nil {
		writeErr(w, http.StatusUnsupportedMediaType, errors.New("not an image"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/"+kind)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store") // a working-tree file changes under the page
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

type contentRow struct {
	Text string      `json:"text"`
	Tok  []tokTriple `json:"tok,omitempty"`
}

type fileContentBody struct {
	Lines    []contentRow `json:"lines"`
	TooLarge bool         `json:"too_large,omitempty"`
	Missing  bool         `json:"missing,omitempty"`
	// Binary: the bytes are no text (the TUI's rule: a NUL or invalid
	// UTF-8) and Lines is empty; Size is the byte count for the placeholder.
	// Image names the format (png/jpeg/gif) with the pixel size when the
	// binary is an image the page can fetch from /api/file-raw.
	Binary bool   `json:"binary,omitempty"`
	Image  string `json:"image,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Size   int    `json:"size,omitempty"`
	// Stamp is a working-tree read's diskStamp, taken BEFORE the read: the
	// lines are never older than it, so a change mid-read still shows.
	Stamp string `json:"stamp,omitempty"`
	// Notes are an agent's notes on a working-tree file, aligned to the
	// bytes just read (agentdocs): lines and notes come from ONE read.
	Notes []noteRow `json:"notes,omitempty"`
}

// noteRow is one agent note as the viewer draws it; Ref is what r copies.
type noteRow struct {
	ID        string `json:"id"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale,omitempty"`
	Author    string `json:"author"`
	Outdated  bool   `json:"outdated,omitempty"`
	Ref       string `json:"ref"`
}

// notesFor aligns path's notes to data, the working-tree bytes just read,
// and returns them for the page (nil when path has none).
func (s *Server) notesFor(ctx context.Context, path string, data []byte) []noteRow {
	// One store call aligns and reads: positions for exactly these bytes,
	// even while a hosting TUI aligns the same path to another read.
	ns := s.docs.AlignedNotes(s.docsRoot(ctx), path, agentdocs.Lines(data))
	if len(ns) == 0 {
		return nil
	}
	out := make([]noteRow, len(ns))
	for i, n := range ns {
		out[i] = noteRow{ID: n.ID, Start: n.Start, End: n.End, Summary: n.Summary, Rationale: n.Rationale,
			Author: n.Author, Outdated: n.Outdated, Ref: agentdocs.NoteReference(n)}
	}
	return out
}

// diskStamp names a stat for the page to compare: "<size>:<mtime ns>",
// "missing", or "" when the stat failed (never a change).
func diskStamp(d diskStat) string {
	switch {
	case !d.known:
		return ""
	case d.missing:
		return "missing"
	}
	return fmt.Sprintf("%d:%d", d.size, d.mod.UnixNano())
}

// worktreeStamp stamps path in the current tree; "" for a path that is not
// local to it (the domain read refuses those).
func (s *Server) worktreeStamp(path string) string {
	if !filepath.IsLocal(filepath.FromSlash(path)) {
		return ""
	}
	return diskStamp(statDisk(s.ofAbs(s.service().Root(), path)))
}

// handleFileStamp is F's preview following the disk (the TUI's watchedDoc):
// one stat, no read — the page re-reads the content only when this changes.
func (s *Server) handleFileStamp(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || !isGitArgSafe(path) || !filepath.IsLocal(filepath.FromSlash(path)) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	writeJSON(w, map[string]string{"stamp": s.worktreeStamp(path)})
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path, src, rev := q.Get("path"), q.Get("src"), q.Get("rev")
	if path == "" || !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path/rev"))
		return
	}
	svc := s.service()
	ctx := readCtx(r)
	var data []byte
	var err error
	stamp := ""
	switch src {
	case "", "worktree":
		// The domain read refuses a path escaping the checkout; only a file
		// that is simply not there reads as missing (the viewer's "(file
		// deleted on disk)"), never an error.
		stamp = s.worktreeStamp(path)
		data, err = readVersion(ctx, svc, "worktree", "", path)
		if errors.Is(err, fs.ErrNotExist) {
			writeJSON(w, fileContentBody{Lines: []contentRow{}, Missing: true, Stamp: stamp})
			return
		}
	case "commit":
		if rev == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a commit version needs rev"))
			return
		}
		data, err = readVersion(ctx, svc, src, rev, path)
	case "shelf":
		if rev == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a shelf version needs rev (the entry id)"))
			return
		}
		data, err = readVersion(ctx, svc, src, rev, path)
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown src "+src))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(data) > domain.MaxDiffBytes {
		writeJSON(w, fileContentBody{Lines: []contentRow{}, TooLarge: true, Stamp: stamp})
		return
	}
	if domain.IsBinary(data) {
		body := fileContentBody{Lines: []contentRow{}, Binary: true, Size: len(data), Stamp: stamp}
		if kind, pw, ph, err := termimg.Probe(data); err == nil {
			body.Image, body.Width, body.Height = kind, pw, ph
		}
		writeJSON(w, body)
		return
	}
	body := fileContentBody{Lines: contentRows(path, data, svc.SyntaxHighlighting()), Stamp: stamp}
	if src == "" || src == "worktree" {
		body.Notes = s.notesFor(ctx, path, data)
	}
	writeJSON(w, body)
}

// readVersion reads path at one version: the bytes ON DISK (a missing file is
// fs.ErrNotExist, passed through), a commit's blob, or a shelf entry's. src
// and rev are the caller's to validate.
func readVersion(ctx context.Context, svc *domain.Service, src, rev, path string) ([]byte, error) {
	switch src {
	case "commit":
		return svc.ShowFile(ctx, rev, path)
	case "shelf":
		return svc.ShelfBlob(ctx, rev)
	}
	return svc.WorktreeFile(ctx, path)
}

// contentRows splits data into lines — "\n"-terminated, a trailing "\r"
// trimmed (CRLF), no phantom line after the last terminator — each with its
// syntax runs when lex is on and the file is lexable (the TUI's lexPreview
// rule: ≤ MaxSyntaxBytes, no bare CR).
func contentRows(path string, data []byte, lex bool) []contentRow {
	text := strings.TrimSuffix(string(data), "\n")
	rows := []contentRow{}
	if len(data) == 0 {
		return rows
	}
	var tok [][]syntax.Tok
	if lex && len(data) <= domain.MaxSyntaxBytes && !domain.HasBareCR(data) {
		if lang := syntax.Detect(path); lang != "" {
			tok = syntax.Lex(lang, data)
		}
	}
	for i, l := range strings.Split(text, "\n") {
		rows = append(rows, contentRow{Text: strings.TrimSuffix(l, "\r"), Tok: tokTriples(tok, i+1)})
	}
	return rows
}
