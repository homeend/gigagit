package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Stashing a SUBSET of the working tree. The plain "stash" op (ophttp.go)
// sends no paths and takes the whole tree with it; this one carries the
// checklist's answer, which is the TUI's stash popup (s on the Files panel).
//
// A separate wire name, not an extra field on "stash": a registered op is
// looked up BEFORE that switch, so registering "stash" would silently replace
// the whole-tree lane the ☰ button and the stash button already use.
func init() {
	RegisterOp("stash-paths", buildStashPaths)
}

// stashCandidate reports whether f can go into a stash, and whether it is
// untracked. Ported from the TUI's stashCandidates (internal/tui/
// stash_popup.go): untracked files and files with unstaged content — a file
// whose only change is already staged has nothing left in the working tree to
// stash, and an unmerged path is not stashable at all (git refuses, and
// stashing half a conflict is never what was meant).
func stashCandidate(f model.FileStatus) (untracked, ok bool) {
	untracked = f.Kind == model.KindUntracked
	if f.Kind == model.KindUnmerged {
		return untracked, false
	}
	return untracked, untracked || (f.Unstaged != '.' && f.Unstaged != 0)
}

// buildStashPaths stashes exactly the paths the checklist ticked. Every path
// is checked against a FRESH status: a path that is not (or is no longer) a
// stash candidate refuses the whole operation rather than being dropped
// quietly, so what lands in the stash is always what the list showed.
//
// -u rides on the selection, not on a separate toggle: git needs it to pick
// up an untracked file, and asking about it twice would be asking the same
// question twice (the TUI derives it the same way).
func buildStashPaths(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
	if len(req.Paths) == 0 {
		return nil, nil, http.StatusBadRequest, errors.New("select at least one file to stash")
	}
	st, err := s.service().Status(readCtx(r))
	if err != nil {
		return nil, nil, http.StatusInternalServerError, err
	}
	cand := make(map[string]bool, len(st.Files)) // path -> untracked
	for _, f := range st.Files {
		if untracked, ok := stashCandidate(f); ok {
			cand[f.Path] = untracked
		}
	}
	includeUntracked := false
	seen := make(map[string]bool, len(req.Paths))
	paths := make([]string, 0, len(req.Paths))
	for _, p := range req.Paths {
		if !isGitArgSafe(p) {
			return nil, nil, http.StatusBadRequest, errors.New("invalid path: " + p)
		}
		untracked, ok := cand[p]
		if !ok {
			return nil, nil, http.StatusUnprocessableEntity,
				errors.New(p + " has nothing unstaged to stash (already staged, conflicted, or gone)")
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		includeUntracked = includeUntracked || untracked
		paths = append(paths, p)
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = "WIP on " + st.Branch // git's own default phrasing, the TUI's prefill
	}
	return engine.Stash{Message: msg, Paths: paths, IncludeUntracked: includeUntracked}, nil, 0, nil
}

type stashRow struct {
	Ref          string `json:"ref"`
	Subject      string `json:"subject"`
	Sha          string `json:"sha,omitempty"`
	UntrackedSha string `json:"untracked_sha,omitempty"`
}

// handleStashes lists stash entries newest-first. Each row carries the
// stash's commit sha, resolved here so the client's left-click needs no
// second request (the tags-row pattern); a resolve failure drops only the
// sha, never the row.
func (s *Server) handleStashes(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	es, err := svc.StashList(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]stashRow, 0, len(es))
	for _, e := range es {
		row := stashRow{Ref: e.Ref, Subject: e.Subject}
		if sha, serr := svc.StashCommit(readCtx(r), e.Ref); serr == nil {
			row.Sha = sha
		}
		// A -u stash stores untracked files in a THIRD parent (a root
		// commit) invisible to the stash commit's first-parent diff;
		// surface it so the client can list and diff those files. The
		// input is the server-owned ref plus a literal — nothing
		// client-sent. No ^3 → rev-parse errors → field omitted.
		if usha, uerr := svc.StashCommit(readCtx(r), e.Ref+"^3"); uerr == nil {
			row.UntrackedSha = usha
		}
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"stashes": rows})
}
