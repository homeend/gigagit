package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// The two things you DO with a stored entry, as operations: put its content
// back somewhere (restore), and re-apply a shelved commit (cherry-pick).
// Everything else about the stores is a plain read/write in bookmarks.go and
// shelf.go — these two need the op transport because they touch the working
// tree and can park a decision (the overwrite confirm, a pick conflict).

// buildRestore resolves the bytes an entry points at and wraps them in a
// WriteFile op. A BOOKMARK resolves live (what it points at today); a SHELF
// entry resolves to the frozen copy — the whole point of the two stores being
// different. A path names one file INSIDE a shelved commit's archive.
func (s *Server) buildRestore(r *http.Request, req opStartRequest) (engine.Operation, int, error) {
	if req.ID == "" {
		return nil, http.StatusBadRequest, errors.New("id required")
	}
	if req.Dest == "" {
		return nil, http.StatusBadRequest, errors.New("dest required")
	}
	// WriteFile addresses the working tree, so dest is REPO-RELATIVE. An
	// absolute path would be silently joined onto the repo root and land in a
	// mirrored subtree ("/repo/tmp/x/f.txt") — refuse it and say so instead.
	if filepath.IsAbs(req.Dest) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(req.Dest)), "../") {
		return nil, http.StatusBadRequest, errors.New("dest must be a path inside the repository, relative to its root")
	}
	svc := s.service()
	ctx := readCtx(r)
	var (
		data []byte
		err  error
	)
	switch req.Store {
	case "bookmarks":
		b, gerr := svc.BookmarkGet(ctx, req.ID)
		if gerr != nil {
			return nil, http.StatusNotFound, gerr
		}
		if b.IsCommit() {
			return nil, http.StatusUnprocessableEntity, errors.New("a commit bookmark has no file content to restore")
		}
		data, err = svc.BookmarkBytes(ctx, b)
	case "shelf":
		e, ferr := svc.ShelfFind(ctx, req.ID)
		if ferr != nil {
			return nil, http.StatusNotFound, ferr
		}
		if req.Path != "" {
			// One member of a shelved commit's archive.
			data, err = svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: req.Path})
		} else if e.IsCommit() {
			return nil, http.StatusUnprocessableEntity, errors.New("this entry holds a commit's files — name the file to restore")
		} else {
			data, err = svc.ShelfBlob(ctx, e.ID)
		}
	default:
		return nil, http.StatusBadRequest, fmt.Errorf("unknown store %q", req.Store)
	}
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	// WriteFile asks before clobbering an existing, DIFFERENT file — that
	// decision parks in the browser modal like any other.
	return engine.WriteFile{Path: req.Dest, Data: data}, 0, nil
}

// buildShelfCherryPick re-applies a shelved commit: a real cherry-pick while
// the commit object still exists, otherwise a replay of the format-patch
// mailbox frozen alongside the files at shelve time (the CLI's
// `gg shelf cherry-pick` lanes, same order). The temp patch file is the
// caller's to clean up, which is why a cleanup func comes back with the op.
func (s *Server) buildShelfCherryPick(r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if req.ID == "" {
		return nil, nil, http.StatusBadRequest, errors.New("id required")
	}
	svc := s.service()
	ctx := readCtx(r)
	e, err := svc.ShelfFind(ctx, req.ID)
	if err != nil {
		return nil, nil, http.StatusNotFound, err
	}
	if !e.IsCommit() {
		return nil, nil, http.StatusUnprocessableEntity, errors.New("this is a shelved file, not a shelved commit")
	}
	sha := e.Origin.Commit
	if _, found, lerr := svc.CommitLookup(ctx, sha); lerr == nil && found {
		return engine.CherryPick{Commits: []string{sha}}, nil, 0, nil
	}
	// The commit is gone (gc'd, or its history was rewritten). The snapshot
	// is what makes the entry still applyable — an entry shelved before patch
	// support, or a merge commit, simply has none.
	if e.PatchSHA == "" {
		return nil, nil, http.StatusUnprocessableEntity,
			errors.New("the original commit no longer exists and this entry has no stored patch (shelved before patch support, or a merge commit)")
	}
	tmp, perr := svc.ShelfPatchFile(ctx, e.ID)
	if perr != nil {
		return nil, nil, http.StatusInternalServerError, perr
	}
	return engine.ApplyPatch{Path: tmp, Mode: engine.ApplyModeCommits}, func() { _ = os.Remove(tmp) }, 0, nil
}
