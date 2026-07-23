package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
)

type opStartRequest struct {
	Op      string `json:"op"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	Tag     string `json:"tag"`
	Path    string `json:"path"`
}

// handleOpStart begins an operation and returns 202 {op_id}. Ops wired so
// far: switch, commit, pull, push, delete-branch, delete-tag, remove-worktree; the switch
// statement is where future ops land.
func (s *Server) handleOpStart(w http.ResponseWriter, r *http.Request) {
	var req opStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	var op engine.Operation
	switch req.Op {
	case "switch":
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		op = engine.SmartSwitch{Branch: req.Branch}
	case "commit":
		if strings.TrimSpace(req.Message) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("message required"))
			return
		}
		op = engine.Commit{Message: req.Message}
	case "pull":
		op = engine.SmartPull{} // current branch, its configured remote, PullAndStay
	case "push":
		// Branch resolved server-side — nothing client-sent reaches argv, and
		// Force is never wire-settable (force is only reachable through the
		// op's own parked push-rejected → push-force decisions).
		branch, berr := s.svc.CurrentBranch(r.Context())
		if berr != nil {
			writeErr(w, http.StatusInternalServerError, berr)
			return
		}
		if branch == "" {
			// Detached HEAD only. An unborn branch still resolves via
			// symbolic-ref, so it dispatches and surfaces git's own refspec
			// error through the op instead.
			writeErr(w, http.StatusConflict, errors.New("push: no current branch (detached HEAD?)"))
			return
		}
		op = engine.Push{Remote: "origin", Branch: branch, SetUpstream: true} // the TUI's exact P dispatch
	case "delete-branch":
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		// The engine confirms ("delete-branch") and forks on an unmerged
		// branch ("branch-unmerged") — both park in the browser modal.
		op = engine.DeleteBranch{Name: req.Branch}
	case "delete-tag":
		if req.Tag == "" || !isGitArgSafe(req.Tag) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid tag"))
			return
		}
		// Decision-free op: the client shows its own confirm before starting.
		op = engine.DeleteTag{Name: req.Tag}
	case "remove-worktree":
		if req.Path == "" {
			writeErr(w, http.StatusBadRequest, errors.New("path required"))
			return
		}
		// The client-sent path is an identifier, not an argument: resolve it
		// against the server's own worktree list so only server-owned values
		// reach git argv (worktree paths legitimately contain characters
		// isGitArgSafe would reject). The engine still guards the current and
		// main worktree.
		wts, werr := s.svc.Worktrees(r.Context())
		if werr != nil {
			writeErr(w, http.StatusInternalServerError, werr)
			return
		}
		found := false
		for _, wt := range wts {
			if wt.Path == req.Path {
				op = engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch}
				found = true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("unknown worktree"))
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown op %q", req.Op))
		return
	}
	run, err := s.startOp(op)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
}

// handleOpEvents streams the run's events as SSE: buffered history first
// (replay), then live, closing after the terminal done event.
func (s *Server) handleOpEvents(w http.ResponseWriter, r *http.Request) {
	run := s.opByID(r.PathValue("id"))
	if run == nil {
		writeErr(w, http.StatusNotFound, errors.New("unknown operation"))
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	history, live, cancel := run.subscribe()
	defer cancel()
	for _, we := range history {
		writeSSE(w, we)
	}
	fl.Flush()
	if live == nil {
		return // already finished; history ended with done
	}
	for {
		select {
		case we, ok := <-live:
			if !ok {
				return
			}
			writeSSE(w, we)
			fl.Flush()
			if we["type"] == "done" {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSE(w http.ResponseWriter, we wireEvent) {
	b, err := json.Marshal(we)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}

type opDecideRequest struct {
	Option string `json:"option"`
}

// handleOpDecide feeds a parked decision. Options are validated against the
// pending request's list — decisions are option-lists only.
func (s *Server) handleOpDecide(w http.ResponseWriter, r *http.Request) {
	run := s.opByID(r.PathValue("id"))
	if run == nil {
		writeErr(w, http.StatusNotFound, errors.New("unknown operation"))
		return
	}
	var req opDecideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	switch err := run.decide(req.Option); err {
	case nil:
		writeJSON(w, map[string]any{})
	case errBadOption:
		writeErr(w, http.StatusBadRequest, err)
	default: // errNotWaiting, errOpDone
		writeErr(w, http.StatusConflict, err)
	}
}
