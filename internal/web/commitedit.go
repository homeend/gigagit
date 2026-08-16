package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/rebaseplan"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/commit-squash", writeGuard(s.handleCommitSquash))
	})
}

// commitEdits maps the wire verb to the plan builder's edit. The client never
// sends a rebase plan — it names one of these three edits and the plan is
// built here, from a range this server reads itself.
var commitEdits = map[string]rebaseplan.Edit{
	"drop":      rebaseplan.EditDrop,
	"move-up":   rebaseplan.EditMoveUp,
	"move-down": rebaseplan.EditMoveDown,
}

// execPath resolves the gg binary git will re-invoke as the rebase sequence
// editor. It is os.Executable behind a seam because under `go test` that is
// the TEST binary, which would re-run the suite instead of writing a todo
// file — the same reason internal/engine's tests build a real gg.
var execPath = os.Executable

// isHexSha reports whether s is a plain git object name. The commit id reaches
// git argv inside a revision expression (`<sha>~1`), so "does not start with a
// dash" — enough for a branch name — is not enough here: any other rev
// expression would let a client choose the rebase base itself. Every value the
// commits feed hands the browser is already full hex.
func isHexSha(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// buildCommitEdit turns a single-commit history edit (drop / move up / move
// down) into the InteractiveRebase that performs it, mirroring the TUI's
// commit_rebase_ops.go: the rebase base comes from rebaseplan.OntoFor, the
// plan from the onto..branch range read here. It returns the HTTP status to
// report alongside any error — a malformed request is 400, a request that is
// well-formed but cannot apply (root commit, commit not on this branch,
// detached HEAD) is 422, and nothing has run in either case.
func (s *Server) buildCommitEdit(ctx context.Context, svc *domain.Service, sha, edit string) (engine.Operation, int, error) {
	e, ok := commitEdits[edit]
	if !ok {
		return nil, http.StatusBadRequest, fmt.Errorf("unknown edit %q", edit)
	}
	if !isHexSha(sha) {
		return nil, http.StatusBadRequest, errors.New("invalid commit id")
	}
	branch, err := svc.CurrentBranch(ctx)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if branch == "" {
		return nil, http.StatusUnprocessableEntity, errors.New("no branch is checked out — a history edit rewrites a branch")
	}
	onto := rebaseplan.OntoFor(sha, e)
	commits, err := svc.CommitRange(ctx, onto, branch)
	if err != nil {
		// The base does not resolve: the root commit (no parent to rebase
		// onto), or the oldest commit for a move-down (no grandparent).
		return nil, http.StatusUnprocessableEntity, err
	}
	plan, err := rebaseplan.BuildSingleEdit(commits, sha, e)
	if err != nil {
		// "commit is not on the current branch" / "already the newest commit".
		return nil, http.StatusUnprocessableEntity, err
	}
	// The sequence editor is this binary re-invoked as `gg __rebase-seq`; the
	// web server is served from the same cmd/gg binary that routes it.
	ggBin, err := execPath()
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return engine.InteractiveRebase{Branch: branch, Onto: onto, Plan: plan, GGBin: ggBin}, 0, nil
}

// squashRequest names the marked commits to fold into one, NEWEST FIRST — the
// order the feed already holds them in. The order is a hint about where the
// range starts, not something to trust: buildSquash reads the real range from
// git and BuildSquash's membership check refuses anything the hint got wrong.
type squashRequest struct {
	Shas []string `json:"shas"`
}

// handleCommitSquash folds the marked commits into one.
//
// It has an endpoint of its own rather than an /api/op verb because the wire
// carries a LIST of commit ids, and the shared op request has no field that
// means that — reusing one that means something else is how a payload ends up
// being interpreted by the wrong builder. The started run is an ordinary
// operation: the client follows the same SSE stream, so a conflict parks the
// same decision modal it would from anywhere else (the review lane's precedent
// for starting a run from its own endpoint).
func (s *Server) handleCommitSquash(w http.ResponseWriter, r *http.Request) {
	var req squashRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	op, code, err := s.buildSquash(readCtx(r), s.service(), req.Shas)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	run, rerr := s.startOp(op)
	if rerr != nil {
		writeErr(w, http.StatusConflict, rerr)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"op_id": run.id})
}

// buildSquash turns a set of marked commits into the InteractiveRebase that
// folds them into one, mirroring the TUI's commitSquashRow: the oldest target
// seeds the rebase base (oldest^), the range onto..branch is READ HERE, and
// rebaseplan.BuildSquash enforces from that true range that every target is on
// the branch and that the targets are adjacent. Nothing plan-shaped comes off
// the wire.
//
// It returns the HTTP status to report alongside any error: a malformed request
// is 400, a well-formed one that cannot apply (fewer than two commits, a target
// that is not on this branch, a non-adjacent selection, the root commit,
// detached HEAD) is 422, and nothing has run in either case.
func (s *Server) buildSquash(ctx context.Context, svc *domain.Service, shas []string) (engine.Operation, int, error) {
	var targets []string
	seen := map[string]bool{}
	for _, sha := range shas {
		if !isHexSha(sha) {
			return nil, http.StatusBadRequest, errors.New("invalid commit id")
		}
		if seen[sha] {
			continue
		}
		seen[sha] = true
		targets = append(targets, sha)
	}
	if len(targets) < 2 {
		return nil, http.StatusUnprocessableEntity, errors.New("mark at least 2 commits to squash")
	}
	branch, err := svc.CurrentBranch(ctx)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	if branch == "" {
		return nil, http.StatusUnprocessableEntity, errors.New("no branch is checked out — a squash rewrites a branch")
	}
	// The last target is the oldest (the client sends feed order). Its parent
	// is the rebase base; a root commit has none, and git says so.
	onto := targets[len(targets)-1] + "^"
	commits, err := svc.CommitRange(ctx, onto, branch)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, fmt.Errorf("cannot squash from there (the oldest marked commit may be the root): %w", err)
	}
	plan, err := rebaseplan.BuildSquash(commits, targets)
	if err != nil {
		// "commit is not on the current branch" / "commits are not adjacent".
		return nil, http.StatusUnprocessableEntity, err
	}
	ggBin, err := execPath()
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return engine.InteractiveRebase{Branch: branch, Onto: onto, Plan: plan, GGBin: ggBin}, 0, nil
}
