package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/rebaseplan"
)

// Interactive rebase: the plan editor's read side and its op.
//
// ORDER. `rebaseplan.Plan.Entries` is git todo order — OLDEST first — and so
// is everything on this wire, in both directions. The editor displays
// newest-first (the TUI's irebaseEditor does the same) and reverses at its own
// display boundary; nothing between the two ends flips it.
//
// TRUST. The client may REORDER and ANNOTATE the plan; it may not invent it.
// Every plan is checked against a freshly read range: same commits, same
// count, nothing else. That check is also the staleness guard — commit to the
// branch while the editor is open and the plan is refused rather than applied
// to a history that has moved (the hunk-staging hash precedent).

// rebaseCommit is one commit offered to the plan editor.
type rebaseCommit struct {
	Sha     string `json:"sha"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Message string `json:"message"` // full message, so reword can edit the body
}

// handleRebaseRange lists onto..branch for the plan editor, oldest first.
// Both names are resolved against the server's own branch list — the compare
// endpoint's allowlist rule: an unknown name would otherwise come back as an
// empty range, indistinguishable from "these are already equal".
func (s *Server) handleRebaseRange(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	branch, onto := r.URL.Query().Get("branch"), r.URL.Query().Get("onto")
	if !isGitArgSafe(branch) || !isGitArgSafe(onto) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
		return
	}
	if branch == onto {
		writeErr(w, http.StatusBadRequest, errors.New("a branch cannot be rebased onto itself"))
		return
	}
	bs, err := svc.Branches(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if branchTip(bs, branch) == "" {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no such branch: %s", branch))
		return
	}
	if branchTip(bs, onto) == "" {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no such branch: %s", onto))
		return
	}
	cs, err := svc.CommitRange(r.Context(), onto, branch)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]rebaseCommit, 0, len(cs))
	for _, c := range cs {
		short := c.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		rows = append(rows, rebaseCommit{Sha: c.Hash, Short: short, Subject: c.Subject, Message: c.Message})
	}
	writeJSON(w, map[string]any{"branch": branch, "onto": onto, "commits": rows})
}

// planEntry is one wire row of a submitted plan.
type planEntry struct {
	Sha    string `json:"sha"`
	Action string `json:"action"` // pick | reword | squash | drop
	Msg    string `json:"msg"`    // reword only: the new full message
}

var planActions = map[string]rebaseplan.Action{
	"pick":   rebaseplan.Pick,
	"reword": rebaseplan.Reword,
	"squash": rebaseplan.Squash,
	"drop":   rebaseplan.Drop,
}

// buildInteractiveRebase validates a client plan against a freshly read range
// and turns it into the op. It returns the HTTP status to report with any
// error: 400 for a malformed plan, 409 when the plan no longer describes the
// branch (it moved under the open editor), 404 for an unknown branch. Nothing
// runs in any of those cases.
func (s *Server) buildInteractiveRebase(ctx context.Context, svc *domain.Service, branch, onto string, plan []planEntry) (engine.Operation, int, error) {
	if !isGitArgSafe(branch) {
		return nil, http.StatusBadRequest, errors.New("invalid branch")
	}
	if !isGitArgSafe(onto) {
		return nil, http.StatusBadRequest, errors.New("invalid base branch")
	}
	if len(plan) == 0 {
		return nil, http.StatusBadRequest, errors.New("empty plan")
	}
	bs, err := svc.Branches(ctx)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if branchTip(bs, branch) == "" {
		return nil, http.StatusNotFound, fmt.Errorf("no such branch: %s", branch)
	}
	cs, err := svc.CommitRange(ctx, onto, branch)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}
	// The plan must name exactly the commits in the range — every one, once.
	// A short plan means the branch moved (or the client dropped a row by
	// deleting it, which the editor never does: dropping is an ACTION).
	remaining := make(map[string]string, len(cs)) // sha → full message
	for _, c := range cs {
		remaining[c.Hash] = c.Message
	}
	if len(plan) != len(cs) {
		return nil, http.StatusConflict, fmt.Errorf("the plan covers %d commits but the branch now has %d — reopen the editor", len(plan), len(cs))
	}
	entries := make([]rebaseplan.Entry, 0, len(plan))
	for i, p := range plan {
		action, ok := planActions[p.Action]
		if !ok {
			return nil, http.StatusBadRequest, fmt.Errorf("unknown action %q", p.Action)
		}
		orig, known := remaining[p.Sha]
		if !known {
			// Either not in the range at all, or named twice.
			return nil, http.StatusConflict, fmt.Errorf("commit %s is not in %s..%s — reopen the editor", p.Sha, onto, branch)
		}
		delete(remaining, p.Sha)
		if action == rebaseplan.Squash && i == 0 {
			// Nothing older to meld into — the TUI's editor refuses the same.
			return nil, http.StatusBadRequest, errors.New("the oldest commit has nothing to squash into")
		}
		if action == rebaseplan.Reword && p.Msg == "" {
			return nil, http.StatusBadRequest, errors.New("reword needs a message")
		}
		// Orig comes from the range read, never from the client: it is what
		// the sequence editor writes back for a pick, and a client-supplied
		// value would let the wire rewrite a message it never showed.
		entries = append(entries, rebaseplan.Entry{Sha: p.Sha, Action: action, Orig: orig, NewMsg: p.Msg})
	}
	ggBin, err := execPath()
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return engine.InteractiveRebase{
		Branch: branch,
		Onto:   onto,
		Plan:   rebaseplan.Plan{Entries: entries},
		GGBin:  ggBin,
	}, 0, nil
}
