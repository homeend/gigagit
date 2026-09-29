package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/engine"
)

// Recycle a worktree: check a branch out in an existing worktree the page is
// not on (engine.RecycleWorktree). The branch and remote rows' "recycle a
// worktree…" pick the worktree; a dirty target parks the recycle.dirty
// decision (commit / shelve / discard / abort) in the ordinary modal.
func init() { RegisterOp("recycle-worktree", buildRecycleWorktree) }

// buildRecycleWorktree resolves every wire value against a fresh read (the
// remove-worktree / checkout-remote allowlist pattern): path must be a listed
// non-bare worktree, branch a listed local branch, ref a listed remote
// branch. Only the local name for a remote branch is free text, for git's
// own check-ref-format. The engine keeps the rest of the refusals (the
// worktree the page is on, a paused op, a lock, a branch checked out).
func buildRecycleWorktree(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	svc := s.service()
	ctx := r.Context()
	if req.Path == "" {
		return nil, nil, http.StatusBadRequest, errors.New("path required")
	}
	if (req.Branch == "") == (req.Ref == "") {
		return nil, nil, http.StatusBadRequest, errors.New("exactly one of branch or ref is required")
	}
	wts, err := svc.Worktrees(ctx)
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	dir := ""
	for _, wt := range wts {
		if wt.Path == req.Path && !wt.Bare {
			dir = wt.Path
			break
		}
	}
	if dir == "" {
		return nil, nil, http.StatusNotFound, errors.New("unknown worktree")
	}
	if req.Branch != "" {
		bs, err := svc.Branches(ctx)
		if err != nil {
			return nil, nil, http.StatusInternalServerError, err
		}
		for _, b := range bs {
			if b.Name == req.Branch {
				return engine.RecycleWorktree{Dir: dir, Branch: b.Name}, nil, 0, nil
			}
		}
		return nil, nil, http.StatusNotFound, errors.New("unknown branch")
	}
	if req.Name == "" || !isGitArgSafe(req.Name) {
		return nil, nil, http.StatusBadRequest, errors.New("invalid branch name")
	}
	rbs, err := svc.RemoteBranches(ctx)
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	for _, rb := range rbs {
		if rb.Name == req.Ref {
			return engine.RecycleWorktree{Dir: dir, Branch: req.Name, RemoteRef: rb.Name}, nil, 0, nil
		}
	}
	return nil, nil, http.StatusNotFound, errors.New("unknown remote branch")
}
