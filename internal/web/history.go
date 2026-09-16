package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/syntax"
)

// Commit-feed parity reads: resolve a rev to its full sha (goto-sha), one
// file's commit history (the history overlay), and per-line blame (the blame
// overlay). All read-only GETs — hostGuard applies as everywhere, no
// writeGuard (the /api/compare, /api/versions precedent). rev/path values
// reach git argv, so isGitArgSafe gates each one; an EMPTY rev is legal
// where documented (filelog: HEAD; blame: the working tree) and is not run
// through isGitArgSafe, which rejects empty strings.

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	rev := r.URL.Query().Get("rev")
	if !isGitArgSafe(rev) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid rev"))
		return
	}
	svc := s.service()
	sha, found, err := svc.ResolveRev(r.Context(), rev)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, errors.New("unknown revision: "+rev))
		return
	}
	// Subject is display garnish on the fallback path; a failed lookup only
	// costs the subject, never the resolve.
	subject := ""
	if line, ok, _ := svc.CommitLookup(r.Context(), sha); ok {
		subject = line.Subject
	}
	writeJSON(w, map[string]any{"hash": sha, "subject": subject})
}

type fileLogRow struct {
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Status  string `json:"status"` // A M D R C T — the file's change at this commit
	Path    string `json:"path"`   // the file's name AT this commit (post-rename)
	OldPath string `json:"old_path,omitempty"`
}

func (s *Server) handleFileLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path, rev := q.Get("path"), q.Get("rev")
	if !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path/rev"))
		return
	}
	limit := 200
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 1000 {
		limit = n
	}
	fcs, err := s.service().FileLog(r.Context(), rev, path, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]fileLogRow, 0, len(fcs))
	for _, fc := range fcs {
		short := fc.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		rows = append(rows, fileLogRow{
			Hash: fc.Hash, Short: short, Subject: fc.Subject,
			Author: fc.Author, Time: fc.UnixTime,
			Status: fc.Status, Path: fc.Path, OldPath: fc.OldPath,
		})
	}
	writeJSON(w, map[string]any{"rows": rows})
}

type blameRow struct {
	Hash    string `json:"hash"`  // "" = not yet committed
	Short   string `json:"short"` // "" when Hash is ""
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Summary string `json:"summary"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
	// Tok is the line's syntax runs in /api/diff's left_tok/right_tok shape
	// ([start, end, class]); absent when the line has no styled runs, the
	// file has no lexer, or [ui] diff_syntax is off. renderCell paints it.
	Tok []tokTriple `json:"tok,omitempty"`
}

func (s *Server) handleBlame(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	path, rev := q.Get("path"), q.Get("rev")
	if !isGitArgSafe(path) || (rev != "" && !isGitArgSafe(rev)) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path/rev"))
		return
	}
	lines, err := s.service().Blame(r.Context(), rev, path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Lexed per request (the blame LRU caches lines, not runs); chroma costs
	// ~1 s at domain.MaxSyntaxBytes, which is the same budget the diff pays.
	var tok [][]syntax.Tok
	if s.service().SyntaxHighlighting() {
		tok = domain.LexBlameLines(path, lines)
	}
	rows := make([]blameRow, 0, len(lines))
	for _, l := range lines {
		short := l.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		rows = append(rows, blameRow{
			Hash: l.Hash, Short: short, Author: l.Author,
			Time: l.Time, Summary: l.Summary, Line: l.LineNo, Text: l.Content,
			Tok: tokTriples(tok, l.LineNo),
		})
	}
	writeJSON(w, map[string]any{"lines": rows})
}
