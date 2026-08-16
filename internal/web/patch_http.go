package web

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/homeend/gigagit/internal/domain"
)

// The read half of the patch feature: a commit, or one file's change within a
// commit, downloaded as a .patch — plus the prefill the copy-to-temp-dir
// prompt opens with.
//
// Both are plain GETs and register themselves (routereg.go) rather than being
// added to the route table in server.go. Neither mutates, so neither takes
// writeGuard; hostGuard covers them like every other endpoint.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/commit-patch", s.handleCommitPatch)
		mux.HandleFunc("GET /api/export-dest", s.handleExportDest)
	})
}

// handleCommitPatch serves `git format-patch -1 --binary` for sha as a file
// download: the whole commit, or — with a path — just that file's change
// within it.
//
// A patch is BYTES. It must not go through writeJSON: the browser saves what
// comes back verbatim, and a JSON-wrapped patch is not one git can read.
//
// A merge commit is refused up front (ErrMergeCommitPatch) because
// format-patch -1 does not error on one — it silently emits a DIFFERENT
// commit's patch, so the download would look fine and be wrong.
func (s *Server) handleCommitPatch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sha, path := q.Get("sha"), q.Get("path")
	if !isHexSha(sha) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid sha"))
		return
	}
	// path is optional; when present it reaches git argv like any other.
	if path != "" && !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	svc := s.service()
	var (
		data []byte
		name string
		err  error
	)
	if path == "" {
		data, name, err = svc.CommitPatch(r.Context(), sha)
	} else {
		data, name, err = svc.FilePatch(r.Context(), sha, path)
	}
	if err != nil {
		// The merge refusal is about what was asked for, not a server fault:
		// 422 with git's own sentence, which the client shows as-is.
		if errors.Is(err, domain.ErrMergeCommitPatch) {
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("this commit produced an empty patch"))
		return
	}
	// strconv.Quote, not a hand-rolled `"`+name+`"`: the file name carries a
	// path basename in the per-file case, and an unescaped quote or backslash
	// in it would break the header.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// handleExportDest answers with the directory the copy-to-temp-dir prompt
// should open prefilled: `<main-worktree>.tmp/<entry name>`, exactly what the
// TUI's popup prefills.
//
// The client cannot compute this from /api/repo: that reports TopLevel, which
// is the LINKED worktree when gg web is served from one, while TempExportBase
// deliberately anchors on the main worktree so the copies of one repository
// all land in one place.
func (s *Server) handleExportDest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	files, name, code, err := s.entryExport(r, q.Get("store"), q.Get("id"))
	if err != nil {
		writeErr(w, code, err)
		return
	}
	base, err := s.service().TempExportBase(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"dir": filepath.Join(base, name), "files": len(files)})
}
