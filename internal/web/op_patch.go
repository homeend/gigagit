package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Patch import + entry export, as operations.
//
// Both register themselves (opreg.go) rather than adding an arm to the
// operation switch in ophttp.go — see that file's comment for why.
//
// Two directions a change enters or leaves this frontend: apply-patch reads a
// .patch file the user NAMES and lands it, either in the working tree or as
// real commits; export-to-dir writes a bookmark's or shelf entry's files into
// a directory outside the repository (the TUI's `t`).
//
// The read direction (a commit or one file's diff downloaded as a .patch) is
// bytes, not an operation, and lives in patch_http.go.
func init() {
	RegisterOp("apply-patch", buildApplyPatch)
	RegisterOp("export-to-dir", buildExportToDir)
}

// applyModes maps the wire value to the engine's mode. The empty string is
// ApplyModeAuto and is what the client always sends: a plain diff then goes to
// the working tree and a format-patch mailbox raises the engine's OWN
// apply_patch.mode decision, which parks in the browser modal. Pre-deciding
// that in the client would mean sniffing the file over HTTP and answering a
// question the engine is already equipped to ask.
var applyModes = map[string]engine.ApplyPatchMode{
	"":             engine.ApplyModeAuto,
	"auto":         engine.ApplyModeAuto,
	"working-tree": engine.ApplyModeWorkingTree,
	"commits":      engine.ApplyModeCommits,
}

// buildApplyPatch imports the patch file at req.Path.
//
// The path is a SERVER-side path the user typed. A browser cannot hand a
// server the path behind an <input type=file> — it only has the bytes — and
// this server is loopback-only, so naming a path is the honest lane and the
// same one the TUI's palette offers. It is passed to the engine exactly as
// typed (relative resolves against the server's working directory, absolute
// is absolute); silently rewriting it would make a failure impossible to read.
func buildApplyPatch(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if req.Path == "" {
		return nil, nil, http.StatusBadRequest, errors.New("path required: name the .patch file to apply")
	}
	// The path reaches `git am` / `git apply` argv, so a value git would read
	// as an option is refused before any verb sees it.
	if !isGitArgSafe(req.Path) {
		return nil, nil, http.StatusBadRequest, errors.New("invalid patch path")
	}
	mode, ok := applyModes[req.Mode]
	if !ok {
		return nil, nil, http.StatusBadRequest, fmt.Errorf("unknown apply mode %q", req.Mode)
	}
	// A missing file, a mailbox in the wrong mode and a conflicted apply are
	// all the engine's to report: it says which, in its own words, over the
	// event stream. Nothing is pre-checked here so those sentences survive.
	return engine.ApplyPatch{Path: req.Path, Mode: mode}, nil, 0, nil
}

// buildExportToDir writes a stored entry's files into req.Dest.
//
// The destination is deliberately OUTSIDE the repository (the default is
// `<main-worktree>.tmp/<name>`, see handleExportDest), which is what
// distinguishes this from restore-entry in entryops.go: that one puts content
// back INTO the working tree at a repo-relative path, this one copies it
// somewhere you can hand to another tool. An existing directory raises the
// engine's own overwrite decision, which parks in the modal.
func buildExportToDir(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if req.Dest == "" {
		return nil, nil, http.StatusBadRequest, errors.New("dest required: the directory to write into")
	}
	files, _, code, err := s.entryExport(r, req.Store, req.ID)
	if err != nil {
		return nil, nil, code, err
	}
	return engine.ExportToDir{Dir: req.Dest, Files: files}, nil, 0, nil
}

// entryExport resolves one store entry into the files to write plus the
// default subdirectory name for it. Shared by the operation above and the
// prefill endpoint, so the name the prompt offers and the files that are
// written can never disagree.
//
// A BOOKMARK resolves live (a commit bookmark archives what that commit
// changes today); a SHELF entry resolves to the frozen copy. Same split as
// restore-entry — it is the whole reason the two stores exist.
func (s *Server) entryExport(r *http.Request, store, id string) (files []model.ExportFile, name string, code int, err error) {
	if id == "" {
		return nil, "", http.StatusBadRequest, errors.New("id required")
	}
	svc := s.service()
	ctx := readCtx(r)
	switch store {
	case "bookmarks":
		b, gerr := svc.BookmarkGet(ctx, id)
		if gerr != nil {
			return nil, "", http.StatusNotFound, gerr
		}
		files, name, err = svc.ExportBookmark(ctx, b)
	case "shelf":
		e, ferr := svc.ShelfFind(ctx, id)
		if ferr != nil {
			return nil, "", http.StatusNotFound, ferr
		}
		files, name, err = svc.ExportShelfEntry(ctx, e)
	default:
		return nil, "", http.StatusBadRequest, fmt.Errorf("unknown store %q", store)
	}
	if err != nil {
		// "this commit changes no files" and a missing blob are refusals about
		// the entry, not server faults — 422 so the client shows the sentence
		// rather than a generic failure.
		return nil, "", http.StatusUnprocessableEntity, err
	}
	if len(files) == 0 {
		return nil, "", http.StatusUnprocessableEntity, errors.New("this entry holds no files to copy")
	}
	return files, name, 0, nil
}
