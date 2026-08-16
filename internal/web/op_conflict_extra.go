package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Whole-file conflict resolution, as operations.
//
// The browser already has the hunk picker, which is the right tool when the
// two sides have to be woven together line by line. It is the WRONG tool when
// you already know which side wins — and it cannot help at all with a
// modify/delete conflict, where there are no markers to pick through. These
// are the TUI's one-key answers (C/i/m/k/d/b in the conflict popup):
//
//	resolve-conflict    one file, one whole-file action
//	mark-all-resolved   stage every conflicted file exactly as it stands
//
// Both are registered ops (opreg.go) in this file rather than arms of
// ophttp.go's switch, so this feature never touches a shared file. Neither
// name exists in that switch — a registered name is consulted FIRST and would
// silently shadow it.
func init() {
	RegisterOp("resolve-conflict", buildResolveConflict)
	RegisterOp("mark-all-resolved", buildMarkAllResolved)
}

// conflictActionFor maps a wire action name onto the engine action for THIS
// file, honoring its conflict class. It mirrors the TUI's conflictActionFor
// (internal/tui/conflict_process.go) key-for-key — that function is the one
// behavioural definition of what a class allows, and the two must not drift:
//
//	both-sides (UU, AA)              ours · theirs · mark
//	modify/delete (DU, UD, AU, UA)   keep · delete · base¹
//	deleted on both sides (DD)       delete · base¹
//
// ¹ base only where stage 1 exists.
//
// ok=false means "not offered for this file", which the caller answers with a
// 422 naming what IS offered — never a silent no-op, and never a resolution
// the class cannot express.
func conflictActionFor(f model.FileStatus, action string) (engine.ConflictAction, bool) {
	both := f.ConflictClass() == model.ConflictBothSides
	switch action {
	case "ours":
		return engine.KeepOurs, both
	case "theirs":
		return engine.KeepTheirs, both
	case "mark":
		return engine.MarkResolved, both
	case "keep":
		// The side that HAS content — "keep the file" reads the same to the
		// user whichever side did the deleting. both-deleted has no side to
		// keep, which is what the hasSide guard is for.
		if both || !(f.ConflictHasOurs() || f.ConflictHasTheirs()) {
			return 0, false
		}
		if f.ConflictHasTheirs() {
			return engine.KeepTheirs, true
		}
		return engine.KeepOurs, true
	case "delete":
		return engine.DeleteFile, !both
	case "base":
		return engine.KeepBase, !both && f.ConflictHasBase()
	}
	return 0, false
}

// conflictActionNames lists the wire actions a file accepts, in menu order.
// It is what the refusal message quotes, and what the client's own gating is
// checked against (conflict_actions_js_test.go).
func conflictActionNames(f model.FileStatus) []string {
	var out []string
	for _, a := range []string{"ours", "theirs", "mark", "keep", "delete", "base"} {
		if _, ok := conflictActionFor(f, a); ok {
			out = append(out, a)
		}
	}
	return out
}

// conflictedFile finds path in a freshly-read status and insists it is
// unmerged. The two failure codes follow the discard/hunk-picker precedent:
// a path git has never heard of is a 404, a path that is simply not
// conflicted (already resolved in another tab) is a 422.
func conflictedFile(svc *domain.Service, r *http.Request, path string) (model.FileStatus, int, error) {
	if path == "" || !isGitArgSafe(path) {
		return model.FileStatus{}, http.StatusBadRequest, errors.New("invalid path")
	}
	st, err := svc.Status(readCtx(r))
	if err != nil {
		return model.FileStatus{}, http.StatusInternalServerError, err
	}
	for _, f := range st.Files {
		if f.Path != path {
			continue
		}
		if f.Kind != model.KindUnmerged {
			return model.FileStatus{}, http.StatusUnprocessableEntity, errors.New(path + " is not conflicted")
		}
		return f, 0, nil
	}
	return model.FileStatus{}, http.StatusNotFound, errors.New("unknown path: " + path)
}

func buildResolveConflict(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	// The action rides on Mode: opStartRequest is a shared file this feature
	// may not extend, and Mode is already the "which variant of this op"
	// field (reset's soft/mixed/hard, the worktree keep modes).
	f, code, err := conflictedFile(s.service(), r, req.Path)
	if err != nil {
		return nil, nil, code, err
	}
	action, ok := conflictActionFor(f, req.Mode)
	if !ok {
		return nil, nil, http.StatusUnprocessableEntity, fmt.Errorf(
			"%q is not offered for %s (%s) — this conflict takes: %v",
			req.Mode, req.Path, f.ConflictLabel(), conflictActionNames(f))
	}
	return engine.ResolveConflict{Path: req.Path, Action: action}, nil, 0, nil
}

// buildMarkAllResolved stages every conflicted path AS IT STANDS — markers
// included, if the user left any. The path list is derived from a fresh
// status HERE rather than sent by the client: a stale list from a page that
// has not refreshed would either miss a conflict (leaving the op unable to
// continue) or name a file that has since become an ordinary change and stage
// it by accident. Nothing but the currently-unmerged set can be staged this
// way.
func buildMarkAllResolved(s *Server, r *http.Request, _ opStartRequest) (engine.Operation, func(), int, error) {
	st, err := s.service().Status(readCtx(r))
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	var paths []string
	for _, f := range st.Files {
		if f.Kind == model.KindUnmerged {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return nil, nil, http.StatusUnprocessableEntity, errors.New("nothing is conflicted")
	}
	return engine.MarkAllResolved{Paths: paths}, nil, 0, nil
}
