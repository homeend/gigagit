package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/syntax"
)

// The viewer's read (open files, plan 5a): one file's lines at one version —
// the working tree (the bytes ON DISK), a commit, or a shelf entry — with the
// syntax runs /api/blame ships, so renderCell paints both alike.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/file-content", s.handleFileContent)
	})
}

type contentRow struct {
	Text string      `json:"text"`
	Tok  []tokTriple `json:"tok,omitempty"`
}

type fileContentBody struct {
	Lines    []contentRow `json:"lines"`
	TooLarge bool         `json:"too_large,omitempty"`
	Missing  bool         `json:"missing,omitempty"`
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
	switch src {
	case "", "worktree":
		top, terr := svc.TopLevel(ctx)
		if terr == nil {
			if _, serr := os.Stat(filepath.Join(top, filepath.FromSlash(path))); errors.Is(serr, os.ErrNotExist) {
				writeJSON(w, fileContentBody{Lines: []contentRow{}, Missing: true})
				return
			}
		}
		data, err = svc.WorktreeFile(ctx, path)
	case "commit":
		if rev == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a commit version needs rev"))
			return
		}
		data, err = svc.ShowFile(ctx, rev, path)
	case "shelf":
		if rev == "" {
			writeErr(w, http.StatusBadRequest, errors.New("a shelf version needs rev (the entry id)"))
			return
		}
		data, err = svc.ShelfBlob(ctx, rev)
	default:
		writeErr(w, http.StatusBadRequest, errors.New("unknown src "+src))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(data) > domain.MaxDiffBytes {
		writeJSON(w, fileContentBody{Lines: []contentRow{}, TooLarge: true})
		return
	}
	writeJSON(w, fileContentBody{Lines: contentRows(path, data, svc.SyntaxHighlighting())})
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
