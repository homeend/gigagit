package web

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/homeend/gigagit/internal/engine"
)

// The worktree operations the browser could not reach: moving one (which is
// also how a rename is done) and cutting one that KEEPS the start commit's
// changes rather than including them.
//
// These register themselves (opreg.go) rather than adding arms to
// handleOpStart's switch. Note the names: `create-worktree` already exists in
// that switch and lookupOp is consulted FIRST, so registering that name here
// would silently shadow both of its existing lanes. The keep lane is its own
// wire name for exactly that reason.
func init() {
	RegisterOp("move-worktree", buildMoveWorktree)
	RegisterOp("create-worktree-keep", buildCreateWorktreeKeep)
}

// buildMoveWorktree backs both the rename and the move rows: same op, and the
// client is what decides which destination a given intention produces.
//
// Both paths must be ABSOLUTE. The engine compares them against the worktree
// list to refuse the main worktree and a nested destination, and a relative
// path would be resolved against whatever cwd the server happens to have —
// a different repository, in the worst case. The refusal is the frontend's
// job because the wire is where a bad value enters.
func buildMoveWorktree(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if !isGitArgSafe(req.Path) || !filepath.IsAbs(req.Path) {
		return nil, nil, http.StatusBadRequest, errors.New("worktree path must be absolute")
	}
	if !isGitArgSafe(req.Dest) || !filepath.IsAbs(req.Dest) {
		return nil, nil, http.StatusBadRequest, errors.New("destination must be absolute")
	}
	return engine.MoveWorktree{Path: req.Path, Dest: req.Dest}, nil, 0, nil
}

// worktreeKeepModes is the allowlist the wire's `mode` resolves through. An
// integer off the wire would let a future engine constant be selected by a
// client that predates it; a name that is not in this map is a 400.
var worktreeKeepModes = map[string]engine.WorktreeKeep{
	"staged":   engine.KeepStaged,
	"unstaged": engine.KeepUnstaged,
}

// buildCreateWorktreeKeep cuts a worktree at a commit's PARENT with that
// commit's diff left staged or unstaged in it — the TUI's keep modes. The
// engine refuses a root or merge commit (WorktreeKeepParentError) before
// anything is created; the client does not offer the rows there either, so
// this is the belt to that braces.
func buildCreateWorktreeKeep(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if !isHexSha(req.Sha) {
		return nil, nil, http.StatusBadRequest, errors.New("invalid commit")
	}
	if req.Name == "" || !isGitArgSafe(req.Name) {
		return nil, nil, http.StatusBadRequest, errors.New("invalid branch name")
	}
	if req.Path == "" || !isGitArgSafe(req.Path) {
		return nil, nil, http.StatusBadRequest, errors.New("invalid path")
	}
	keep, ok := worktreeKeepModes[req.Mode]
	if !ok {
		return nil, nil, http.StatusBadRequest, fmt.Errorf("unknown keep mode %q", req.Mode)
	}
	return engine.CreateWorktree{
		StartPoint:     req.Sha,
		Branch:         req.Name,
		Path:           req.Path,
		Keep:           keep,
		PostCreateHook: s.postCreateHook(r),
	}, nil, 0, nil
}
