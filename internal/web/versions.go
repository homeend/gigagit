package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/changeset"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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
	body := map[string]any{
		"ref":     rep.Ref,
		"checked": rep.Checked,
		"drifted": rep.Checked && rep.Report.Drifted(),
		"added":   driftEntryRows(rep.Report.Added),
		"removed": driftEntryRows(rep.Report.Removed),
	}
	// The compared version's preview link — the drift panel's copy button,
	// the TUI notice's "Copy preview link". Absent for a one-branch record
	// or nothing recorded (the button is then not offered).
	if rep.Version.Base != "" {
		repo, repoErr := s.service().LinkRepo(r.Context())
		if link, ok := versionLink(repo, repoErr, rep.Version); ok {
			body["link"], body["desc"] = link.String(), domain.VersionLinkDesc(branch, rep.Version)
		}
	}
	writeJSON(w, body)
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
	// Link is the version's preview link (gg://<repo>@<base>..<ours>?version=<id>)
	// and Desc what copying it records; both empty for a one-branch record
	// or a checkout the grammar cannot spell (the menu row is then not offered).
	Link string `json:"link,omitempty"`
	Desc string `json:"desc,omitempty"`
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
	repo, repoErr := s.service().LinkRepo(r.Context())
	for _, v := range vs {
		short := v.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		row := versionRow{
			Ref: v.Ref, Hash: v.Hash, Short: short,
			Subject: v.Subject, Op: v.Op, Unix: v.Unix,
			Source: v.Source, Target: v.Target,
		}
		if link, ok := versionLink(repo, repoErr, v); ok {
			// The desc is spelled locally: DescribeLink would run one
			// for-each-ref per row to look up what this row already is.
			row.Link, row.Desc = link.String(), domain.VersionLinkDesc(branch, v)
		}
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"branch": branch, "versions": rows})
}

// versionLink builds a version's preview link — the pair it froze plus the
// ?version= hint — as a model.Link, never by concatenation. false for a
// one-branch record, a trailer that does not hold two usable shas (it is
// never sha-checked on write), or a checkout the grammar cannot spell.
func versionLink(repo model.LinkRepo, repoErr error, v model.BranchVersion) (model.Link, bool) {
	if repoErr != nil || v.Base == "" || v.Ours == "" {
		return model.Link{}, false
	}
	if _, err := model.CommitEndpoint(v.Base); err != nil {
		return model.Link{}, false
	}
	if _, err := model.CommitEndpoint(v.Ours); err != nil {
		return model.Link{}, false
	}
	l := model.Link{
		Repo:   repo,
		Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: v.Base, B: v.Ours}},
		Side:   model.NoteSideNew,
		Hint:   model.LinkHint{Kind: "version", ID: v.ID()},
	}
	if _, err := model.ParseLink(l.String()); err != nil {
		return model.Link{}, false
	}
	return l, true
}

// handleVersionFind answers a landed ?version= hint for the page, which
// holds no branch to ask /api/versions with: domain.FindVersion's ladder
// (id tie-broken by the pair, else the pair). A miss is 200 found:false —
// the hint degrades on the page, it never fails the landing.
func (s *Server) handleVersionFind(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, a, b := q.Get("id"), q.Get("a"), q.Get("b")
	if !model.LinkHintIDOK(id) || !isFullSha(a) || !isFullSha(b) {
		writeErr(w, http.StatusBadRequest, errors.New("version-find needs id and two full shas"))
		return
	}
	branch, v, ok, err := s.service().FindVersion(r.Context(), id, a, b)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeJSON(w, map[string]any{"found": false})
		return
	}
	writeJSON(w, map[string]any{"found": true, "branch": branch, "ref": v.Ref})
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
