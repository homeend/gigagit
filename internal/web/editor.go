package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/editor-open", writeGuard(s.handleEditorOpen))
	})
}

// editorOpenReq is one "open in external editor" (the TUI's Edit in editor /
// e / I): source names what the editor gets — a live working-tree file or a
// read-only copy of the index, a commit's file, a commit's message, a
// bookmark, a shelf entry, or a working-tree file as a copy (blame).
type editorOpenReq struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Rev    string `json:"rev"`
	ID     string `json:"id"`
	Line   int    `json:"line"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

// startEditor is the editor start: s.editorStart in tests, else the domain's
// editor session — directly, in hosted mode too (Sessions() is process-wide
// and an editor needs no steer inbox or agent channel).
func (s *Server) startEditor(svc *domain.Service, argv []string, label, dir, cwd string, cols, rows int, done func()) (string, error) {
	if s.editorStart != nil {
		return s.editorStart(svc, argv, label, dir, cwd, cols, rows, done)
	}
	sess, err := svc.StartEditor(context.Background(), argv, label, dir, cwd, cols, rows, done) // outlives the request
	if err != nil {
		return "", err
	}
	return string(sess.Info().ID), nil
}

// handleEditorOpen runs the server's $VISUAL / $EDITOR — never a command the
// request names — in a console session: on the working-tree file itself
// (live), or on a read-only copy removed when the editor exits.
func (s *Server) handleEditorOpen(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var q editorOpenReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	ctx := readCtx(r)
	pathOK := func() bool {
		p := q.Path
		return p != "" && isGitArgSafe(p) && !filepath.IsAbs(p) && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, `\`)
	}
	var (
		target, name string
		live         bool
		data         []byte
		err          error
		code         = http.StatusNotFound
	)
	switch q.Source {
	case "worktree":
		if !pathOK() {
			writeErr(w, http.StatusBadRequest, errors.New("path must be a file inside the working tree"))
			return
		}
		if target, err = svc.WorktreeAbsPath(ctx, q.Path); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		name, live = q.Path, true
	case "staged", "worktree-copy", "commit":
		if !pathOK() {
			writeErr(w, http.StatusBadRequest, errors.New("path must be a file inside the repository"))
			return
		}
		name = q.Path
		switch q.Source {
		case "staged":
			data, err = svc.ShowFile(ctx, "", q.Path) // `git show :path` = the index blob
		case "worktree-copy":
			data, err = svc.WorktreeFile(ctx, q.Path)
		default:
			if !isHexSha(q.Rev) {
				writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
				return
			}
			data, err = svc.ShowFile(ctx, q.Rev, q.Path)
		}
	case "message":
		if !isHexSha(q.Rev) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
			return
		}
		var msg string
		msg, err = svc.CommitMessage(ctx, q.Rev)
		data, name = []byte(msg), "COMMIT_EDITMSG" // the name nudges editors into commit-message highlighting
	case "bookmark":
		b, gerr := svc.BookmarkGet(ctx, q.ID)
		if gerr != nil {
			writeErr(w, http.StatusNotFound, gerr)
			return
		}
		if b.IsCommit() {
			writeErr(w, http.StatusUnprocessableEntity, errors.New("a commit bookmark has no file to open"))
			return
		}
		data, err = svc.BookmarkBytes(ctx, b)
		name, code = b.Path, http.StatusInternalServerError
	case "shelf":
		e, ferr := svc.ShelfFind(ctx, q.ID)
		if ferr != nil {
			writeErr(w, http.StatusNotFound, ferr)
			return
		}
		switch {
		case q.Path != "":
			if !pathOK() {
				writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
				return
			}
			data, err = svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: q.Path})
			name = q.Path
		case e.IsArchive():
			writeErr(w, http.StatusUnprocessableEntity, errors.New("this entry holds several files — name the file to open"))
			return
		default:
			data, err = svc.ShelfBlob(ctx, e.ID)
			name = e.Origin.Path
		}
		code = http.StatusInternalServerError
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown source %q", q.Source))
		return
	}
	if err != nil {
		writeErr(w, code, err)
		return
	}
	var done func()
	if !live {
		if len(data) > domain.MaxDiffBytes {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("file too large to open (%d bytes)", len(data)))
			return
		}
		if target, err = domain.WriteViewCopy(name, data); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		copyPath := target
		done = func() { domain.RemoveViewCopy(copyPath) }
	}
	dir := svc.Root()
	cwd, err := s.sessionPlace(dir)
	if err != nil {
		if done != nil {
			done()
		}
		writeErr(w, http.StatusConflict, err)
		return
	}
	cols, rows := startSize(q.Cols, q.Rows)
	argv := domain.EditorArgv(domain.ResolveEditor(os.Getenv), target, q.Line)
	id, err := s.startEditor(svc, argv, "Editor: "+filepath.Base(name), dir, cwd, cols, rows, done)
	if err != nil {
		if done != nil {
			done()
		}
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("%s: %w", argv[0], err))
		return
	}
	out := map[string]any{"id": id, "name": filepath.Base(name), "live": live}
	for _, sw := range s.sessionsNow() {
		if sw.ID == id {
			out["session"] = sw
		}
	}
	writeJSON(w, out)
}
