package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

type opStartRequest struct {
	Op      string `json:"op"`
	Branch  string `json:"branch"`
	Onto    string `json:"onto"`
	Message string `json:"message"`
	Tag     string `json:"tag"`
	Path    string `json:"path"`
	Ref     string `json:"ref"`
	Sha     string `json:"sha"`
	Name    string `json:"name"` // new branch name (create-branch, rename-branch)
	Force   bool   `json:"force"`
}

// handleOpStart begins an operation and returns 202 {op_id}. Ops wired so
// far: switch, commit, fetch, pull, push, merge, rebase, create-branch,
// rename-branch, create-worktree, delete-branch, delete-tag, remove-worktree,
// stash, stash-apply, stash-pop, stash-drop, discard, restore-version,
// delete-version; the switch statement is
// where future ops land. pull and push each take an OPTIONAL branch — omitted
// means the current one.
func (s *Server) handleOpStart(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
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
	case "merge":
		// Drag-and-drop pair: Branch is the dragged source, Onto the branch
		// it was dropped on. Both names are validated here; the engine then
		// checks they exist and refuses source == target itself.
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		if req.Onto == "" || !isGitArgSafe(req.Onto) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid target branch"))
			return
		}
		// SmartMerge is worktree-aware: it merges in place, or inside the
		// worktree holding the target, or autostashes and switches — so an
		// arbitrary pair works with no client-side precondition. A conflict
		// forks "merge-conflict" into the parking modal.
		op = engine.SmartMerge{Source: req.Branch, Target: req.Onto}
	case "rebase":
		// Branch is the dragged branch — the one REWRITTEN and ended on —
		// and Onto the branch it was dropped on. Unlike merge, the ladder
		// pivots on Branch, so the labels must not be swapped.
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		if req.Onto == "" || !isGitArgSafe(req.Onto) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid base branch"))
			return
		}
		// A conflict pauses the replay and forks "rebase-conflict" into the
		// parking modal.
		op = engine.SmartRebase{Branch: req.Branch, Onto: req.Onto}
	case "commit":
		if strings.TrimSpace(req.Message) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("message required"))
			return
		}
		op = engine.Commit{Message: req.Message}
	case "fetch":
		op = engine.Fetch{} // all remotes; no arguments, no decisions
	case "create-branch":
		// Only the leading-dash check here: the engine runs the new name
		// through git check-ref-format and reports a clear refusal, which is
		// a better error than any allowlist this layer could invent.
		if req.Name == "" || !isGitArgSafe(req.Name) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch name"))
			return
		}
		if req.Branch != "" && !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid start point"))
			return
		}
		op = engine.CreateBranch{Name: req.Name, StartPoint: req.Branch} // "" = HEAD
	case "rename-branch":
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		if req.Name == "" || !isGitArgSafe(req.Name) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch name"))
			return
		}
		op = engine.RenameBranch{Old: req.Branch, New: req.Name}
	case "create-worktree":
		// For an EXISTING branch: the engine refuses a branch that is not
		// local, and git itself refuses one already checked out elsewhere.
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		if req.Path == "" || !isGitArgSafe(req.Path) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
			return
		}
		// The configured post-create hook is honoured rather than silently
		// skipped, so the web behaves like the TUI. It is not run unopposed:
		// the engine's approval decision shows the script and parks in the
		// browser modal, defaulting to skip on anything but an explicit run.
		op = engine.CreateWorktreeForBranch{
			Branch:         req.Branch,
			Path:           req.Path,
			PostCreateHook: s.postCreateHook(r),
		}
	case "pull":
		// No branch = the current one, checked out and stayed on (the header
		// button and palette). A named branch pulls WITHOUT leaving the
		// current one — SmartPull sends the current branch down its own lane
		// anyway when the two coincide, so the client needs no precondition.
		if req.Branch == "" {
			op = engine.SmartPull{} // current branch, its configured remote, PullAndStay
			break
		}
		if !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		op = engine.SmartPull{Branch: req.Branch, Intent: engine.PullInBackground}
	case "push":
		// `force` does NOT force-push. engine.Push{Force:true} asks the
		// push-force decision (force-with-lease / force / abort) and pushes
		// whatever comes back, so the flag only reaches that prompt — the
		// same one the rejection-recovery path already parks in the browser
		// modal — without needing a rejection first. A silent force is not
		// expressible on this wire.
		branch := req.Branch
		if branch == "" {
			// No branch named: resolve the current one server-side.
			cur, berr := svc.CurrentBranch(r.Context())
			if berr != nil {
				writeErr(w, http.StatusInternalServerError, berr)
				return
			}
			if cur == "" {
				// Detached HEAD only. An unborn branch still resolves via
				// symbolic-ref, so it dispatches and surfaces git's own refspec
				// error through the op instead.
				writeErr(w, http.StatusConflict, errors.New("push: no current branch (detached HEAD?)"))
				return
			}
			branch = cur
		} else if !isGitArgSafe(branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		op = engine.Push{Remote: "origin", Branch: branch, SetUpstream: true, Force: req.Force} // the TUI's exact P dispatch
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
		wts, werr := svc.Worktrees(r.Context())
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
	case "stash":
		// All changes incl. untracked (the common case; path-scoped stashing
		// stays TUI-only). Message optional; nothing-to-stash surfaces git's
		// own error through the op.
		op = engine.Stash{Message: req.Message, IncludeUntracked: true}
	case "stash-apply", "stash-pop", "stash-drop":
		if req.Ref == "" {
			writeErr(w, http.StatusBadRequest, errors.New("ref required"))
			return
		}
		// The client-sent ref is an identifier: resolve it against the
		// server's own stash list so only server-owned values reach git argv
		// (the remove-worktree allowlist pattern). All three ops are
		// decision-free; drop's confirm lives client-side.
		entries, serr := svc.StashList(r.Context())
		if serr != nil {
			writeErr(w, http.StatusInternalServerError, serr)
			return
		}
		found := false
		for _, e := range entries {
			if e.Ref == req.Ref {
				// Optional freshness guard: the client sends the sha it
				// listed; a successful resolve that mismatches means the
				// stash list changed under it (stash@{N} is positional).
				// A resolve error does not block — best-effort only.
				if req.Sha != "" {
					if cs, cerr := svc.StashCommit(r.Context(), e.Ref); cerr == nil && cs != req.Sha {
						writeErr(w, http.StatusConflict, errors.New("stash list changed; refresh"))
						return
					}
				}
				switch req.Op {
				case "stash-apply":
					op = engine.StashApply{Ref: e.Ref}
				case "stash-pop":
					op = engine.StashPop{Ref: e.Ref}
				default:
					op = engine.StashDrop{Ref: e.Ref}
				}
				found = true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("unknown stash"))
			return
		}
	case "restore-version":
		// Move a branch back to a recorded snapshot. The engine snapshots the
		// pre-restore tip first (a restore is itself undoable), refuses a
		// branch checked out in another worktree, and forks "restore-dirty"
		// into the parking modal when the current branch has uncommitted
		// changes. It also verifies the ref BELONGS to the branch, so the two
		// values cannot be crossed.
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		if req.Ref == "" || !isGitArgSafe(req.Ref) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid version ref"))
			return
		}
		op = engine.RestoreBranchVersion{Branch: req.Branch, Ref: req.Ref}
	case "delete-version":
		// Removes one snapshot ref. Decision-free — the client confirms first
		// (the delete-tag convention). The engine refuses any ref outside
		// refs/gg/versions/, which is what keeps a client bug from deleting a
		// real branch or tag through this op.
		if req.Ref == "" || !isGitArgSafe(req.Ref) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid version ref"))
			return
		}
		op = engine.DeleteBranchVersion{Ref: req.Ref}
	case "discard":
		// Per-file discard. The path resolves against a fresh status read
		// (the remove-worktree/stash allowlist pattern): a stale client row
		// 404s instead of discarding the wrong thing. Decision-free — the
		// client confirms before POSTing (the delete-tag convention).
		if req.Path == "" || !isGitArgSafe(req.Path) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
			return
		}
		st, err := svc.Status(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		var discard engine.Operation
		for _, f := range st.Files {
			if f.Path != req.Path {
				continue
			}
			switch f.Kind {
			case model.KindUnmerged:
				writeErr(w, http.StatusUnprocessableEntity, errors.New("conflicted — resolve instead"))
				return
			case model.KindUntracked:
				discard = engine.Discard{Remove: []string{req.Path}}
			default:
				discard = engine.Discard{Restore: []string{req.Path}}
			}
			break
		}
		if discard == nil {
			writeErr(w, http.StatusNotFound, errors.New("unknown path"))
			return
		}
		op = discard
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

// postCreateHook reads the repo's configured worktree post-create hook, the
// same probe feedFor uses for commit-sort (committed .gg.toml only; the
// machine-local private repo config is not consulted). Any failure yields ""
// — no hook — because a config read must never block creating a worktree.
//
// Returning the script rather than "" is deliberate: skipping it silently
// would make the web quietly diverge from the TUI. The engine gates it behind
// an approval decision that shows the script and defaults to skip, so it
// reaches the browser modal before anything runs.
func (s *Server) postCreateHook(r *http.Request) string {
	top, err := s.service().TopLevel(r.Context())
	if err != nil {
		return ""
	}
	cfg, err := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml"))
	if err != nil {
		return ""
	}
	return cfg.Worktree.PostCreateHook
}
