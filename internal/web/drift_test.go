package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// driftRepo builds main (f.txt) and feature (feature.txt added), main having
// diverged with a real file change — unlike divergedRepo's --allow-empty
// commits, a drift comparison needs ACTUAL changed paths to compare.
func driftRepo(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t, 1) // main: f.txt = "content 1"
	gitRun(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feature work")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main work")
	return dir
}

type versionPreviewResp struct {
	Left      string `json:"left"`
	Right     string `json:"right"`
	NoPreview bool   `json:"no_preview"`
}

func getVersionPreview(t *testing.T, ts *httptest.Server, ref string) versionPreviewResp {
	t.Helper()
	var body versionPreviewResp
	if code := getJSON(t, ts, "/api/version-preview?ref="+ref, &body); code != http.StatusOK {
		t.Fatalf("GET /api/version-preview?ref=%s = %d", ref, code)
	}
	return body
}

// The endpoint must answer the two hashes a two-branch op recorded — left =
// the merge base at snapshot time, right = the branch's own contribution
// (Ours) — and never the live-preview reconciled endpoints (previews.go),
// which would render against TODAY's tips instead of what gg recorded.
func TestVersionPreviewReturnsTwoHashes(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	wantBase := gitRun(t, dir, "merge-base", "main", "feature")
	wantRight := gitRun(t, dir, "rev-parse", "feature")
	ts := serve(t, New(domain.Open(dir)))

	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	ref := listVersions(t, ts, "main").Versions[0].Ref

	got := getVersionPreview(t, ts, ref)
	if got.NoPreview {
		t.Fatalf("no_preview = true, want a real preview for a two-branch merge")
	}
	if got.Left != wantBase {
		t.Errorf("left = %s, want the merge base %s", got.Left, wantBase)
	}
	if got.Right != wantRight {
		t.Errorf("right = %s, want feature's tip %s", got.Right, wantRight)
	}
}

// A one-branch op's version record (restore-version, here) records no
// endpoints — domain.ErrNoPreview is a SHAPE, not a failure, so the endpoint
// must answer 200 with no_preview:true rather than an error.
func TestVersionPreviewNoPreviewForOneBranchOp(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	mergeRef := listVersions(t, ts, "main").Versions[0].Ref
	runOpOK(t, ts, `{"op":"restore-version","branch":"main","ref":"`+mergeRef+`"}`)

	after := listVersions(t, ts, "main").Versions
	if len(after) != 2 || after[0].Op != "restore" {
		t.Fatalf("versions after restore = %+v, want newest op restore", after)
	}
	got := getVersionPreview(t, ts, after[0].Ref)
	if !got.NoPreview {
		t.Fatalf("no_preview = false for a restore's record, want true (one-branch op)")
	}
	if got.Left != "" || got.Right != "" {
		t.Errorf("left/right = %q/%q on a no_preview response, want both empty", got.Left, got.Right)
	}
}

// The version row itself must carry Source/Target: v.Hash/v.Short name the
// SNAPSHOTTED branch's own old tip, not either side VersionPreview returns
// (left = merge-base, right = the source's contribution), so the client
// needs the branch names — not that hash — to label the compare view
// honestly.
func TestVersionsRowIncludesSourceTarget(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)

	var body struct {
		Versions []struct {
			Source string `json:"source"`
			Target string `json:"target"`
		} `json:"versions"`
	}
	if code := getJSON(t, ts, "/api/versions?branch=main", &body); code != http.StatusOK {
		t.Fatalf("GET /api/versions = %d", code)
	}
	if len(body.Versions) != 1 {
		t.Fatalf("versions = %+v, want 1", body.Versions)
	}
	if body.Versions[0].Source != "feature" || body.Versions[0].Target != "main" {
		t.Errorf("source/target = %q/%q, want feature/main", body.Versions[0].Source, body.Versions[0].Target)
	}
}

func TestVersionPreviewRejects(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	if code := getJSON(t, ts, "/api/version-preview", nil); code != http.StatusBadRequest {
		t.Errorf("no ref = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/version-preview?ref=--all", nil); code != http.StatusBadRequest {
		t.Errorf("leading dash = %d, want 400", code)
	}
}

type driftEntry struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type driftResp struct {
	Ref     string       `json:"ref"`
	Checked bool         `json:"checked"`
	Drifted bool         `json:"drifted"`
	Added   []driftEntry `json:"added"`
	Removed []driftEntry `json:"removed"`
}

func getDrift(t *testing.T, ts *httptest.Server, branch string) driftResp {
	t.Helper()
	var body driftResp
	if code := getJSON(t, ts, "/api/drift?branch="+branch, &body); code != http.StatusOK {
		t.Fatalf("GET /api/drift?branch=%s = %d", branch, code)
	}
	return body
}

// Nothing recorded (no versions written yet) must report Checked=false, not
// an error — DriftAfter's own contract.
func TestDriftNothingRecorded(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	got := getDrift(t, ts, "main")
	if got.Checked || got.Drifted {
		t.Fatalf("drift on a branch with nothing recorded = %+v, want checked=false", got)
	}
}

// A commit landing on main AFTER the recorded merge, introducing a path the
// merge itself never touched, is exactly the drift this feature exists to
// catch: DriftAfter compares the branch's newest recorded version against
// its tip now, and the endpoint must surface the added path.
func TestDriftEndpointReportsAddedEntries(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)

	if got := getDrift(t, ts, "main"); got.Drifted {
		t.Fatalf("drift right after the merge = %+v, want not drifted yet", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "sneaky.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "sneaky change")

	got := getDrift(t, ts, "main")
	if !got.Checked || !got.Drifted {
		t.Fatalf("drift after the extra commit = %+v, want checked+drifted", got)
	}
	var found bool
	for _, e := range got.Added {
		if e.Path == "sneaky.txt" && e.Status == "A" {
			found = true
		}
	}
	if !found {
		t.Errorf("added = %+v, want sneaky.txt A", got.Added)
	}
}

func TestDriftRejectsMissingBranch(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	if code := getJSON(t, ts, "/api/drift", nil); code != http.StatusBadRequest {
		t.Errorf("no branch = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/drift?branch=--all", nil); code != http.StatusBadRequest {
		t.Errorf("leading dash = %d, want 400", code)
	}
}

// Both new endpoints must keep the standing loopback Host guard — a new
// route is exactly the kind of place that forgets to wire it back in (the
// preflight/status precedent).
func TestVersionPreviewAndDriftRejectForeignHost(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	ref := listVersions(t, ts, "main").Versions[0].Ref

	for _, path := range []string{"/api/version-preview?ref=" + ref, "/api/drift?branch=main"} {
		req, err := http.NewRequest("GET", ts.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = "evil.example.com"
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("GET %s with a foreign Host = %d, want 403", path, resp.StatusCode)
		}
	}
}

// The panel and its CSS visibility rule must actually exist: a new
// class="hidden" element with no matching #id.hidden rule renders visible
// regardless of the class (the shipped bug this project's per-id hiding rule
// guards against).
func TestDriftPanelHasAMatchingHiddenRule(t *testing.T) {
	t.Parallel()
	html := readPreflightStatic(t, "index.html")
	css := readPreflightStatic(t, "style.css")
	if !strings.Contains(html, `id="drift-panel"`) {
		t.Fatal(`index.html missing id="drift-panel"`)
	}
	if !strings.Contains(html, `id="drift-panel" class="hidden"`) {
		t.Fatal("index.html drift panel must start hidden")
	}
	if !strings.Contains(css, "#drift-panel.hidden") {
		t.Fatal("style.css missing #drift-panel.hidden rule — the panel would ALWAYS be visible (per-id hiding rule)")
	}
}

// Static wiring checks (previewsjs_test.go / preflight_test.go style): there
// is no JS runtime in this test binary, so these are string-containment
// checks that the client actually calls the new endpoints and wires the
// frozen preview into the compare view / commit view fallback.
func TestVersionsJSIsWiredForFrozenPreviewAndDrift(t *testing.T) {
	t.Parallel()
	checks := []struct{ file, want, why string }{
		{"versions.js", `/api/version-preview`, "a version row must fetch its recorded preview"},
		{"versions.js", `no_preview`, "a one-branch op's record must fall back to the commit view"},
		{"versions.js", `openCommitByHash`, "the no-preview fallback must open the commit view"},
		{"versions.js", `openCompare`, "a real preview must open the existing compare view"},
		{"versions.js", `merge-base(`, "the frozen preview must label its base side by branch name, not the snapshot's own hash"},
		{"versions.js", `/api/drift`, "the post-op drift check must call the drift endpoint"},
		{"ops.js", `driftArmFor`, "startOp must decide which branch to drift-check"},
		{"ops.js", `checkDrift`, "a successful drift-eligible op must run the check"},
		// I4: ev.changed alone also matches the engine's deliberate
		// success-with-conflicts shape (changed && !ok), where the branch ref
		// never moved — firing there floods the panel with a D for every path
		// the other side contributed.
		{"ops.js", `ev.ok && ev.changed && op.driftBranch`, "the drift check must gate on ok AND changed, not changed alone"},
		// I5: the spec's second trigger — a resume of an op that paused for
		// conflicts is worth reporting even when nothing drifted. The web CAN
		// resume (op "continue"), so unlike the CLI it must plumb the flag.
		{"ops.js", `driftPaused`, "a resume of a paused op must arm the paused trigger"},
		{"versions.js", `paused for conflicts before completing`, "the paused-only panel needs its own wording"},
		{"versions.js", `but the resolution is worth a look`, "the paused-only panel must say why it is worth a look"},
	}
	for _, c := range checks {
		if got := readPreflightStatic(t, c.file); !strings.Contains(got, c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}
