package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/rebaseplan"
)

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
