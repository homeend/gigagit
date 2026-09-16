package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/domain"
)

// Frozen version previews + drift: the read side of Task 12. A version's
// own recorded endpoints (Left/Right) are a LENS over the compare pipeline —
// never the live-preview open/reconcile path (previews.go), which recomputes
// against today's branch tips. A frozen snapshot has no live tips to
// reconcile against; routing it through that path would render the version
// against the CURRENT tips instead of what gg actually recorded, which is
// exactly the drift this feature exists to detect (see version_preview.go
// and drift.go in internal/domain).
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/version-preview", s.handleVersionPreview)
		mux.HandleFunc("GET /api/drift", s.handleDrift)
	})
}

// handleVersionPreview answers a version ref's frozen PR-style endpoints:
// left = the merge base recorded at snapshot time, right = the branch's
// contribution then. A one-branch op's record (amend, reset, undo-commit,
// delete-branch, restore) has no endpoints by design — domain.ErrNoPreview
// is a SHAPE, not a failure, so it answers 200 with no_preview:true and the
// client opens the commit view instead.
func (s *Server) handleVersionPreview(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("ref")
	if !isGitArgSafe(ref) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid ref"))
		return
	}
	eps, err := s.service().VersionPreview(r.Context(), ref)
	if errors.Is(err, domain.ErrNoPreview) {
		writeJSON(w, map[string]any{"no_preview": true})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"left": eps.Left.Hash(), "right": eps.Right.Hash()})
}

// driftEntryRow is one changed path in a drift comparison.
type driftEntryRow struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

func driftEntryRows(es []changeset.Entry) []driftEntryRow {
	rows := make([]driftEntryRow, 0, len(es))
	for _, e := range es {
		rows = append(rows, driftEntryRow{Status: string(rune(e.Status)), Path: e.Path})
	}
	return rows
}

// handleDrift answers branch's post-op drift comparison (domain.DriftAfter):
// its newest recorded version's change set against what it contributes now.
// Checked is false when there was nothing recorded to compare against (a
// fast-forward pull, an unrecorded branch, or the versions feature itself
// disabled) — not an error, so the client renders "nothing to say" rather
// than a failure.
func (s *Server) handleDrift(w http.ResponseWriter, r *http.Request) {
	branch := r.URL.Query().Get("branch")
	if !isGitArgSafe(branch) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
		return
	}
	rep, err := s.service().DriftAfter(r.Context(), branch)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{
		"ref":     rep.Ref,
		"checked": rep.Checked,
		"drifted": rep.Checked && rep.Report.Drifted(),
		"added":   driftEntryRows(rep.Report.Added),
		"removed": driftEntryRows(rep.Report.Removed),
	})
}

// Branch versions — the operations history. Every destructive op snapshots
// the branch tip to refs/gg/versions/<branch>/<unix>-<op> first, and this is
// the read side of that: what the branch pointed at before each one.
//
// The branch name is NOT resolved against the live branch list, unlike solo
// or remove-worktree. There a bad value broke something (an unrenderable
// scope, a git argv); here BranchVersions is one for-each-ref under a
// prefix, so an unknown branch simply comes back empty and renders as "no
// versions" — and a DELETED branch's versions are exactly what this read is
// for. isGitArgSafe is the whole guard.

type versionRow struct {
	Ref     string `json:"ref"`
	Hash    string `json:"hash"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Op      string `json:"op"` // protocol token: merge, rebase, amend, …
	Unix    int64  `json:"unix"`
	// Source/Target name the two-branch op's sides (empty for a one-branch
	// op, alongside Base/Ours/Other) — Task 12's frozen-preview row needs
	// them to label the compare view honestly: v.Hash/v.Short are the
	// SNAPSHOTTED branch's own old tip, not either side VersionPreview
	// returns (Base/Ours), so labelling by hash would show the wrong sha
	// next to the diff.
	Source string `json:"source"`
	Target string `json:"target"`
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	branch := r.URL.Query().Get("branch")
	if !isGitArgSafe(branch) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
		return
	}
	vs, err := s.service().BranchVersions(r.Context(), branch)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]versionRow, 0, len(vs))
	for _, v := range vs {
		short := v.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		rows = append(rows, versionRow{
			Ref: v.Ref, Hash: v.Hash, Short: short,
			Subject: v.Subject, Op: v.Op, Unix: v.Unix,
			Source: v.Source, Target: v.Target,
		})
	}
	writeJSON(w, map[string]any{"branch": branch, "versions": rows})
}

// vbranchRow is one branch with recorded versions — the all-branches picker
// read, and the only route to a DELETED branch's snapshots (recorded by
// delete-branch itself; restore-version recreates the ref).
type vbranchRow struct {
	Branch     string `json:"branch"`
	Deleted    bool   `json:"deleted"`
	Count      int    `json:"count"`
	LatestUnix int64  `json:"latest_unix"`
}

func (s *Server) handleVersionBranches(w http.ResponseWriter, r *http.Request) {
	vs, err := s.service().AllVersionBranches(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]vbranchRow, 0, len(vs))
	for _, v := range vs {
		rows = append(rows, vbranchRow{
			Branch: v.Branch, Deleted: v.Deleted,
			Count: v.Count, LatestUnix: v.LatestUnix,
		})
	}
	writeJSON(w, map[string]any{"branches": rows})
}
