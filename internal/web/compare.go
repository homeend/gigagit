package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Branch ↔ branch comparison: the whole tip-to-tip changed-file list, each
// file tagged with which side actually touched it since the two diverged.
//
// Both names are resolved to TIP HASHES here, and the client's per-file diffs
// then run against those hashes rather than the names. That is the TUI's rule
// (openBranchCompare): a branch NAME in a diff endpoint poisons the
// session-lived diff cache, which treats a commit↔commit pair as immutable —
// commit to the branch, re-open the same compare, and the stale diff comes
// back.
//
// The names are resolved against the server's own branch list rather than
// merely sanitized (the solo / remove-worktree allowlist precedent):
// isGitArgSafe covers argv, but an unknown name would yield an empty compare
// indistinguishable from "these two branches are identical".

// compareFile is one changed path in a branch comparison.
type compareFile struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	OldPath string `json:"old_path,omitempty"`
	// Origin says which side changed this path since the merge base: "a",
	// "b" or "both". Empty when the origin sets are unavailable (unrelated
	// histories), which the response reports separately.
	Origin string `json:"origin,omitempty"`
}

// branchTip returns name's tip hash, or "" when no such local branch exists.
func branchTip(bs []model.Branch, name string) string {
	for _, b := range bs {
		if b.Name == name {
			return b.Hash
		}
	}
	return ""
}

// pathOrigin classifies a changed path against the two origin sets. A rename
// counts on either of its paths, matching the TUI's filterCompareFiles.
func pathOrigin(o model.CompareOrigins, f model.CommitFile) string {
	in := func(set map[string]bool) bool {
		return set[f.Path] || (f.OldPath != "" && set[f.OldPath])
	}
	a, b := in(o.APaths), in(o.BPaths)
	switch {
	case a && b:
		return "both"
	case a:
		return "a"
	case b:
		return "b"
	}
	return ""
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	a, b := q.Get("a"), q.Get("b")
	if a == "" || b == "" {
		writeErr(w, http.StatusBadRequest, errors.New("a and b are required"))
		return
	}
	if !isGitArgSafe(a) || !isGitArgSafe(b) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
		return
	}
	branches, err := svc.Branches(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	aHash, bHash := branchTip(branches, a), branchTip(branches, b)
	if aHash == "" || bHash == "" {
		writeErr(w, http.StatusNotFound, errors.New("unknown branch"))
		return
	}
	files, err := svc.CompareFiles(r.Context(),
		model.Endpoint{Kind: model.EndpointCommit, Hash: aHash},
		model.Endpoint{Kind: model.EndpointCommit, Hash: bHash})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	origins, originsErr := svc.CompareOrigins(r.Context(), aHash, bHash)
	out := make([]compareFile, len(files))
	for i, f := range files {
		out[i] = compareFile{Path: f.Path, Status: f.Status, OldPath: f.OldPath}
		if originsErr == nil {
			out[i].Origin = pathOrigin(origins, f)
		}
	}
	payload := map[string]any{
		"a": a, "b": b,
		"a_hash": aHash, "b_hash": bHash,
		"files": out,
	}
	// The comparison itself stands without the origin sets — only the
	// per-side filter goes away, so the failure is reported alongside the
	// files rather than instead of them (the TUI keeps the view and makes f
	// inert the same way).
	if originsErr != nil {
		payload["origins_error"] = originsErrText(originsErr)
	}
	writeJSON(w, payload)
}

// originsErrText is the client-facing reason the per-side filter is off.
func originsErrText(err error) string {
	if errors.Is(err, domain.ErrNoMergeBase) {
		return "no common ancestor"
	}
	return err.Error()
}
