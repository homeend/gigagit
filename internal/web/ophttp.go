package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
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
	Name    string `json:"name"` // new branch name (create-branch, rename-branch); user.name (set-identity)
	// create-branch: the picked prefix's identity when the name came from a
	// prefix prefill — its <seq> counters bump after a successful create.
	PrefixID    string `json:"prefix_id"`
	PrefixScope string `json:"prefix_scope"`
	Email       string `json:"email"`  // set-identity: user.email
	Global      bool   `json:"global"` // set-identity: write the global scope instead of the repo's
	Edit        string `json:"edit"`   // commit-edit: drop | move-up | move-down
	Store       string `json:"store"`  // restore-entry: "bookmarks" | "shelf"
	ID          string `json:"id"`     // restore-entry / shelf-cherry-pick: the entry's id
	Dest        string `json:"dest"`   // restore-entry: where the content is written
	// push: tip tags to push after the branch, the client's answer to the
	// "branch tip has tags not on the remote" offer. Verified server-side.
	Tags   []string `json:"tags"`
	Mode   string   `json:"mode"`   // reset: "" (interactive picker) | soft | mixed | hard
	Switch bool     `json:"switch"` // checkout-remote / checkout-remote-head: switch to the new local branch
	Remote string   `json:"remote"` // checkout-remote-head: the remote to fetch the branch from
	Force  bool     `json:"force"`
	Ext    bool     `json:"ext"` // ignore: the whole extension, not the one file
	All    bool     `json:"all"` // discard: everything unstaged, no path
	// Paths is discard's batch form (the marked set). All-or-nothing: any
	// member failing the per-file rules refuses the whole batch.
	Paths []string `json:"paths"`
	// Plan is the interactive-rebase plan, in git todo order (oldest first).
	Plan []planEntry `json:"plan"`
}

// handleOpStart begins an operation and returns 202 {op_id}. Ops wired so
// far: switch, commit, fetch, pull, push, merge, rebase, interactive-rebase,
// commit-edit, create-branch, rename-branch, create-worktree, delete-branch,
// delete-tag, create-tag, annotate-tag, push-tag, delete-remote-tag,
// fast-forward, checkout, reset, checkout-remote, delete-remote-branch,
// reset-remote, prune, remove-worktree, stash, stash-apply, stash-pop,
// stash-drop, discard, ignore, commit-graph, set-identity, abort-apply,
// restore-version,
// delete-version, continue, abort; the switch statement is where future ops
// land. pull and push each take an OPTIONAL branch — omitted means the
// current one.
func (s *Server) handleOpStart(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req opStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	var op engine.Operation
	// A registered op (opreg.go) is consulted first: features declare
	// themselves in their own files instead of adding an arm to the switch
	// below, which is what lets several of them be built in parallel without
	// every branch editing these same lines.
	if build, ok := lookupOp(req.Op); ok {
		built, cleanup, code, berr := build(s, r, req)
		if berr != nil {
			writeErr(w, code, berr)
			return
		}
		if cleanup != nil {
			run, rerr := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
				defer cleanup()
				res, err := svc.Execute(ctx, built, events, dec)
				return res, nil, err
			})
			if rerr != nil {
				writeErr(w, http.StatusConflict, rerr)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
			return
		}
		run, rerr := s.startOp(built)
		if rerr != nil {
			writeErr(w, http.StatusConflict, rerr)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
		return
	}
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
	case "interactive-rebase":
		// The full plan editor. The client may reorder and annotate; the plan
		// is checked against a freshly read range before anything runs.
		built, code, err := s.buildInteractiveRebase(r.Context(), svc, req.Branch, req.Onto, req.Plan)
		if err != nil {
			writeErr(w, code, err)
			return
		}
		op = built
	case "commit-edit":
		// A single-commit history edit on the checked-out branch. The plan is
		// built server-side from a range read here (buildCommitEdit) — the
		// wire carries a commit id and one of three verbs, never a plan.
		built, code, err := s.buildCommitEdit(r.Context(), svc, req.Sha, req.Edit)
		if err != nil {
			writeErr(w, code, err)
			return
		}
		op = built
	case "commit":
		if strings.TrimSpace(req.Message) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("message required"))
			return
		}
		op = engine.Commit{Message: req.Message}
	case "cherry-pick":
		// Apply one commit onto the checked-out branch. Hex-only (the
		// checkout lane's rule). A conflict parks the engine's keep/abort
		// decision in the modal, so no local confirm is required here — the
		// client shows one anyway, because a menu click should not start a
		// sequencer operation unannounced.
		if !isHexSha(req.Sha) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
			return
		}
		op = engine.CherryPick{Commits: []string{req.Sha}}
	case "revert":
		// Undo a commit by adding its inverse on top; the engine refuses a
		// merge commit and reports an already-undone one.
		if !isHexSha(req.Sha) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
			return
		}
		op = engine.Revert{Commit: req.Sha}
	case "reword":
		// Replace a commit's whole message. HEAD is an amend; anything older
		// is replayed by an interactive rebase, which needs the gg binary as
		// git's sequence editor (the commit-edit lane's requirement).
		if !isHexSha(req.Sha) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
			return
		}
		if strings.TrimSpace(req.Message) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("message required"))
			return
		}
		ggBin, gerr := execPath()
		if gerr != nil {
			writeErr(w, http.StatusInternalServerError, gerr)
			return
		}
		op = engine.Reword{Commit: req.Sha, NewMsg: req.Message, GGBin: ggBin}
	case "undo-last-commit":
		// Ref-only: HEAD moves back one and the work stays staged. The engine
		// refuses when the last reflog entry was not a commit, so an undo
		// cannot silently unwind a merge or a reset.
		op = engine.UndoLastCommit{}
	case "fetch":
		op = engine.Fetch{} // all remotes; no arguments, no decisions
	case "continue":
		// The engine probes which of merge/rebase/cherry-pick/revert is
		// paused and dispatches; nothing paused is its own clear refusal.
		op = engine.ContinueOp{}
	case "abort":
		op = engine.AbortOp{}
	case "abort-apply":
		// Discard a STANDALONE conflicted application (stash apply): only
		// valid when unmerged paths exist and no paused sequencer op owns
		// them — a paused op's conflicts belong to abort (git's own
		// sequencer cleanup), and reset --merge under a clean tree would be
		// a silent no-op the button should never offer.
		st, serr := svc.Status(r.Context())
		if serr != nil {
			writeErr(w, http.StatusInternalServerError, serr)
			return
		}
		if st.Counts().Conflicted == 0 {
			writeErr(w, http.StatusUnprocessableEntity, errors.New("nothing is conflicted"))
			return
		}
		if cs := svc.Conflict(r.Context(), st); cs.Op != "" {
			writeErr(w, http.StatusUnprocessableEntity, fmt.Errorf("a paused %s owns these conflicts — use abort", cs.Op))
			return
		}
		op = engine.AbortApply{}
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
		if req.PrefixID != "" {
			// The name came from a prefix prefill: consume the prefix's <seq>
			// counters AFTER a successful create (the TUI's pendingSeqBump
			// contract — a failed create must not burn a number). The wire
			// carries only the prefix's identity; the seq names derive
			// server-side from a fresh read, so a client cannot bump
			// arbitrary counters.
			scope, ok := parseProfileScope(req.PrefixScope)
			if !ok {
				writeErr(w, http.StatusBadRequest, errors.New("prefix_scope must be global or repo"))
				return
			}
			p, ok := s.prefixByID(r, req.PrefixID, scope)
			if !ok {
				writeErr(w, http.StatusNotFound, errors.New("unknown prefix"))
				return
			}
			name, start := req.Name, req.Branch
			seqNames := domain.PrefixSeqNames(p.Value)
			run, err := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
				res, err := svc.Execute(ctx, engine.CreateBranch{Name: name, StartPoint: start}, events, dec)
				if err == nil {
					_ = svc.BumpPrefixSeqs(ctx, seqNames)
				}
				return res, nil, err
			})
			if err != nil {
				writeErr(w, http.StatusConflict, err)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
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
		// From a COMMIT: a new branch (Name) is cut there and checked out in
		// the new worktree — the commits panel's lane. Hex-only, like every
		// other commit target on the wire.
		if req.Sha != "" {
			if !isHexSha(req.Sha) {
				writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
				return
			}
			if req.Name == "" || !isGitArgSafe(req.Name) {
				writeErr(w, http.StatusBadRequest, errors.New("invalid branch name"))
				return
			}
			if req.Path == "" || !isGitArgSafe(req.Path) {
				writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
				return
			}
			op = engine.CreateWorktree{
				StartPoint:     req.Sha,
				Branch:         req.Name,
				Path:           req.Path,
				PostCreateHook: s.postCreateHook(r),
			}
			break
		}
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
		// Tip tags travelling with the branch (the TUI's "Push branch + tags"):
		// the names are verified against what is actually at the tip, and the
		// tag push is CHAINED — it runs only if the branch push succeeded, in
		// the same run, exactly as the TUI's pendingPushTags does.
		if tags := s.verifiedTipTags(r.Context(), req.Tags); len(tags) > 0 {
			branchOp := engine.Push{Remote: "origin", Branch: branch, SetUpstream: true, Force: req.Force}
			run, rerr := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
				res, err := svc.Execute(ctx, branchOp, events, dec)
				if err != nil {
					return res, nil, err
				}
				res, err = svc.Execute(ctx, engine.PushTags{Remote: "origin", Names: tags}, events, dec)
				return res, nil, err
			})
			if rerr != nil {
				writeErr(w, http.StatusConflict, rerr)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
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
	case "create-tag":
		// New tag at Sha ("" = HEAD), annotated when Message is set. Only the
		// leading-dash check on the name: git's own check-ref-format produces
		// the better refusal (the create-branch precedent). No force on this
		// lane — force-recreate belongs to annotate-tag, where the target
		// comes from a server-side read.
		if req.Tag == "" || !isGitArgSafe(req.Tag) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid tag"))
			return
		}
		if req.Sha != "" && !isGitArgSafe(req.Sha) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
			return
		}
		op = engine.CreateTag{Name: req.Tag, Commit: req.Sha, Message: req.Message}
	case "annotate-tag":
		// Force-recreate an EXISTING tag as annotated at its current target.
		// The tag resolves against a fresh read and the target comes from
		// that entry — never the wire: CreateTag{Force} moves a ref, so a
		// client-supplied target would let one POST retarget any tag.
		if req.Tag == "" || !isGitArgSafe(req.Tag) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid tag"))
			return
		}
		if strings.TrimSpace(req.Message) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("message required"))
			return
		}
		tgs, terr := svc.Tags(r.Context())
		if terr != nil {
			writeErr(w, http.StatusInternalServerError, terr)
			return
		}
		target := ""
		for _, tg := range tgs {
			if tg.Name == req.Tag {
				target = tg.Target
				break
			}
		}
		if target == "" {
			writeErr(w, http.StatusNotFound, errors.New("unknown tag"))
			return
		}
		op = engine.CreateTag{Name: req.Tag, Commit: target, Message: req.Message, Force: true}
	case "push-tag", "delete-remote-tag":
		// Both resolve the tag against a fresh read (the stash/remove-worktree
		// allowlist pattern). The engine resolves the remote itself — auto for
		// one, a Decider pick for several — and delete confirms via its own
		// Decider; both park in the browser modal.
		if req.Tag == "" || !isGitArgSafe(req.Tag) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid tag"))
			return
		}
		tgs, terr := svc.Tags(r.Context())
		if terr != nil {
			writeErr(w, http.StatusInternalServerError, terr)
			return
		}
		known := false
		for _, tg := range tgs {
			if tg.Name == req.Tag {
				known = true
				break
			}
		}
		if !known {
			writeErr(w, http.StatusNotFound, errors.New("unknown tag"))
			return
		}
		if req.Op == "push-tag" {
			op = engine.PushTag{Name: req.Tag}
		} else {
			op = engine.DeleteRemoteTag{Tag: req.Tag}
		}
	case "checkout-tag":
		// Check out a tag — detached (no name) or onto a new branch created
		// at it (the reflog checkout's two lanes, addressed by tag name so
		// the reflog message reads "moving to <tag>"). The tag resolves
		// against a fresh read; only the server-owned name reaches argv.
		if req.Tag == "" || !isGitArgSafe(req.Tag) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid tag"))
			return
		}
		if req.Name != "" && !isGitArgSafe(req.Name) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch name"))
			return
		}
		tgs, terr := svc.Tags(r.Context())
		if terr != nil {
			writeErr(w, http.StatusInternalServerError, terr)
			return
		}
		ref := ""
		for _, tg := range tgs {
			if tg.Name == req.Tag {
				ref = tg.Name
				break
			}
		}
		if ref == "" {
			writeErr(w, http.StatusNotFound, errors.New("unknown tag"))
			return
		}
		op = engine.Checkout{Ref: ref, Branch: req.Name}
	case "fast-forward":
		// Advance the CURRENT branch to another local branch's tip (git merge
		// --ff-only semantics). The branch resolves against a fresh read; the
		// engine refuses a target that is not strictly ahead. Decision-free
		// and never history-rewriting, so the labelled menu row is
		// confirmation enough (the DnD pair-menu standing).
		//
		// The commits panel targets a COMMIT instead — it has no branch name
		// to send. Hex-only (the checkout lane's rule): a commit id is
		// content-addressed, so there is nothing to resolve against a read.
		if req.Sha != "" {
			if !isHexSha(req.Sha) {
				writeErr(w, http.StatusBadRequest, errors.New("invalid commit"))
				return
			}
			op = engine.FastForward{Commit: req.Sha}
			break
		}
		if req.Branch == "" || !isGitArgSafe(req.Branch) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		bs, berr := svc.Branches(r.Context())
		if berr != nil {
			writeErr(w, http.StatusInternalServerError, berr)
			return
		}
		known := func(name string) bool {
			for _, b := range bs {
				if b.Name == name {
					return true
				}
			}
			return false
		}
		if !known(req.Branch) {
			writeErr(w, http.StatusNotFound, errors.New("unknown branch"))
			return
		}
		// The pair lane (drag-drop menu): Branch is the branch to advance,
		// Onto the tip to reach — rebase's field naming. Without Onto, Branch
		// is the tip and the CURRENT branch advances (the original lane).
		if req.Onto != "" {
			if !isGitArgSafe(req.Onto) || !known(req.Onto) {
				writeErr(w, http.StatusNotFound, errors.New("unknown target branch"))
				return
			}
			op = engine.FastForward{Branch: req.Branch, Commit: req.Onto}
			break
		}
		op = engine.FastForward{Commit: req.Branch}
	case "checkout":
		// Check out a commit by id — detached (no name) or onto a new branch
		// created there (the reflog menu's two lanes). The target is hex-only
		// (isHexSha): a commit id is content-addressed, so unlike a positional
		// identifier (stash@{N}) there is no staleness hazard to allowlist
		// against — but names ("main", "HEAD~1") have dedicated ops and are
		// refused here.
		if !isHexSha(req.Sha) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit id"))
			return
		}
		if req.Name != "" && !isGitArgSafe(req.Name) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch name"))
			return
		}
		op = engine.Checkout{Ref: req.Sha, Branch: req.Name}
	case "reset":
		// Move the current branch to a commit id (hex-only, as above). Empty
		// mode keeps the engine's interactive flow — the soft/mixed/hard
		// picker (plus the non-ancestor confirm) parks in the browser modal
		// and IS the deliberate confirmation. A preset mode skips every
		// engine guard, so a client sending one owns the confirm itself
		// (the reset-to-remote-tip lane).
		if !isHexSha(req.Sha) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid commit id"))
			return
		}
		switch req.Mode {
		case "", "soft", "mixed", "hard":
		default:
			writeErr(w, http.StatusBadRequest, errors.New("invalid mode"))
			return
		}
		op = engine.Reset{Commit: req.Sha, Mode: req.Mode}
	case "checkout-remote":
		// Materialize a remote-tracking branch under a local name (fast-
		// forward-safe; the engine refuses a diverged or checked-out local).
		// The ref is an identifier resolved against a fresh read — Remote/
		// Branch never come from the wire (the remove-worktree allowlist
		// pattern). The local name is free text for git check-ref-format.
		if req.Ref == "" || !isGitArgSafe(req.Ref) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid remote ref"))
			return
		}
		if req.Name == "" || !isGitArgSafe(req.Name) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch name"))
			return
		}
		rbs, rerr := svc.RemoteBranches(r.Context())
		if rerr != nil {
			writeErr(w, http.StatusInternalServerError, rerr)
			return
		}
		var ref string
		for _, rb := range rbs {
			if rb.Name == req.Ref {
				ref = rb.Name
				break
			}
		}
		if ref == "" {
			writeErr(w, http.StatusNotFound, errors.New("unknown remote branch"))
			return
		}
		intent := engine.CheckoutStay
		if req.Switch {
			intent = engine.CheckoutSwitch
		}
		op = engine.SmartCheckout{RemoteRef: ref, Local: req.Name, Intent: intent}
	case "delete-remote-branch":
		// Fresh-read resolve as above; the engine's own confirm ("delete-
		// remote-branch") parks in the browser modal before the deletion is
		// pushed, so the client needs no local confirm.
		if req.Ref == "" || !isGitArgSafe(req.Ref) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid remote ref"))
			return
		}
		rbs, rerr := svc.RemoteBranches(r.Context())
		if rerr != nil {
			writeErr(w, http.StatusInternalServerError, rerr)
			return
		}
		found := false
		for _, rb := range rbs {
			if rb.Name == req.Ref {
				op = engine.DeleteRemoteBranch{Remote: rb.Remote, Branch: rb.Branch}
				found = true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("unknown remote branch"))
			return
		}
	case "reset-remote":
		// Hard-reset the current branch to its remote counterpart's tip. This
		// is the one lane that presets Reset's Mode — which skips the engine's
		// picker AND its non-ancestor confirm — so two server-side guards
		// carry the safety: the ref must resolve against a fresh read, and it
		// must be the counterpart of the CHECKED-OUT branch (the TUI's
		// remoteResetRow rule — resetting to another branch's remote would
		// move the wrong branch). The client owns the explicit confirm.
		if req.Ref == "" || !isGitArgSafe(req.Ref) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid remote ref"))
			return
		}
		rbs, rerr := svc.RemoteBranches(r.Context())
		if rerr != nil {
			writeErr(w, http.StatusInternalServerError, rerr)
			return
		}
		var target *model.RemoteBranch
		for i := range rbs {
			if rbs[i].Name == req.Ref {
				target = &rbs[i]
				break
			}
		}
		if target == nil {
			writeErr(w, http.StatusNotFound, errors.New("unknown remote branch"))
			return
		}
		cur, cerr := svc.CurrentBranch(r.Context())
		if cerr != nil {
			writeErr(w, http.StatusInternalServerError, cerr)
			return
		}
		if cur == "" || target.Branch != cur {
			writeErr(w, http.StatusUnprocessableEntity, errors.New(req.Ref+" is not the current branch's remote counterpart"))
			return
		}
		op = engine.Reset{Commit: target.Name, Mode: "hard"}
	case "prune":
		// Drop tracking refs for branches deleted upstream, all remotes.
		// Refs-only and re-fetchable, so no confirm.
		op = engine.Prune{}
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
		// Per-file discard, or — with all:true — everything unstaged. The
		// path resolves against a fresh status read (the remove-worktree/
		// stash allowlist pattern): a stale client row 404s instead of
		// discarding the wrong thing. Decision-free — the client confirms
		// before POSTing (the delete-tag convention).
		if req.All {
			st, err := svc.Status(r.Context())
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			// The TUI's canDiscardAll rule: never during a conflict (a bulk
			// discard would destroy resolution state), and refuse a clean
			// tree rather than reporting a no-op success.
			counts := st.Counts()
			if len(st.Conflicts()) > 0 {
				writeErr(w, http.StatusUnprocessableEntity, errors.New("tree has conflicts — resolve first"))
				return
			}
			if counts.Unstaged == 0 && counts.Untracked == 0 {
				writeErr(w, http.StatusUnprocessableEntity, errors.New("nothing to discard"))
				return
			}
			op = engine.Discard{All: true}
			break
		}
		if len(req.Paths) > 0 {
			// Batch form (the client's marked set), all-or-nothing: every
			// member must pass the per-file rules before anything runs, so
			// a stale mark can never half-discard.
			for _, p := range req.Paths {
				if p == "" || !isGitArgSafe(p) {
					writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
					return
				}
			}
			st, err := svc.Status(r.Context())
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			kind := make(map[string]model.FileKind, len(st.Files))
			for _, f := range st.Files {
				kind[f.Path] = f.Kind
			}
			var restore, remove []string
			for _, p := range req.Paths {
				k, ok := kind[p]
				if !ok {
					writeErr(w, http.StatusNotFound, errors.New("unknown path: "+p))
					return
				}
				switch k {
				case model.KindUnmerged:
					writeErr(w, http.StatusUnprocessableEntity, errors.New("conflicted — resolve instead: "+p))
					return
				case model.KindUntracked:
					remove = append(remove, p)
				default:
					restore = append(restore, p)
				}
			}
			op = engine.Discard{Restore: restore, Remove: remove}
			break
		}
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
	case "ignore":
		// Add an untracked file (or its whole extension, ext:true) to the
		// repo-root .gitignore. The path resolves against a fresh status
		// read like discard's, and must be UNTRACKED — git ignores only
		// untracked paths, so offering this on a tracked file would write a
		// dead pattern (the TUI's untrackedFile gate).
		if req.Path == "" || !isGitArgSafe(req.Path) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
			return
		}
		st, err := svc.Status(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		found := false
		for _, f := range st.Files {
			if f.Path != req.Path {
				continue
			}
			if f.Kind != model.KindUntracked {
				writeErr(w, http.StatusUnprocessableEntity, errors.New("not an untracked file"))
				return
			}
			found = true
			break
		}
		if !found {
			writeErr(w, http.StatusNotFound, errors.New("unknown path"))
			return
		}
		op = engine.Ignore{Path: req.Path, Ext: req.Ext}
	case "set-identity":
		// One decision-free write of user.name/user.email to the chosen
		// scope — the same engine op behind the TUI's identity view. The
		// scope is fixed client-side before the POST (two buttons), so no
		// fork ever parks. Values are free text by design (they're config
		// values, not refs), but a leading dash is still refused before it
		// can reach git argv.
		if req.Name == "" || !isGitArgSafe(req.Name) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid name"))
			return
		}
		if req.Email == "" || !isGitArgSafe(req.Email) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid email"))
			return
		}
		op = engine.SetIdentity{Name: req.Name, Email: req.Email, Global: req.Global}
	case "commit-graph":
		// Write the commit-graph now, then keep it fresh — the TUI notice's
		// write+enable chain (tui.startCommitGraphWriteAndEnable), run
		// server-side inside ONE run so the config key/value never come off
		// the wire: a client cannot write arbitrary git config through this.
		run, err := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
			res, err := svc.Execute(ctx, engine.WriteCommitGraph{}, events, dec)
			if err != nil {
				return res, nil, err
			}
			if _, err := svc.Execute(ctx, engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}, events, dec); err != nil {
				return res, nil, err
			}
			return res, nil, nil
		})
		if err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
		return
	case "restore-entry":
		// Put a stored entry's content back on disk. Restoring a BOOKMARK
		// writes what it points at today; restoring a SHELF entry writes the
		// frozen copy — the distinction between the two stores, made visible.
		built, code, berr := s.buildRestore(r, req)
		if berr != nil {
			writeErr(w, code, berr)
			return
		}
		op = built
	case "shelf-cherry-pick":
		// Re-apply a shelved commit: live cherry-pick while the commit
		// exists, else the frozen format-patch mailbox. The patch lane leaves
		// a temp file behind, so the run owns its cleanup.
		built, cleanup, code, cerr := s.buildShelfCherryPick(r, req)
		if cerr != nil {
			writeErr(w, code, cerr)
			return
		}
		if cleanup != nil {
			run, rerr := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
				defer cleanup()
				res, err := svc.Execute(ctx, built, events, dec)
				return res, nil, err
			})
			if rerr != nil {
				writeErr(w, http.StatusConflict, rerr)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
			return
		}
		op = built
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

// postCreateHook reads the repo's configured worktree post-create hook from
// the ACTIVE repo config (effectiveConfig: the machine-local private file
// when one exists, else the committed .gg.toml) — the same file the settings
// panel writes it to, so a hook saved on a private-config repo is the hook
// that runs. Any failure yields "" — no hook — because a config read must
// never block creating a worktree.
//
// Returning the script rather than "" is deliberate: skipping it silently
// would make the web quietly diverge from the TUI. The engine gates it behind
// an approval decision that shows the script and defaults to skip, so it
// reaches the browser modal before anything runs.
func (s *Server) postCreateHook(r *http.Request) string {
	cfg, err := s.effectiveConfig(r.Context(), s.service())
	if err != nil {
		return ""
	}
	return cfg.Worktree.PostCreateHook
}
