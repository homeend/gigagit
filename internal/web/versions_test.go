package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

type versionsResp struct {
	Branch   string `json:"branch"`
	Versions []struct {
		Ref     string `json:"ref"`
		Hash    string `json:"hash"`
		Short   string `json:"short"`
		Subject string `json:"subject"`
		Op      string `json:"op"`
		Unix    int64  `json:"unix"`
	} `json:"versions"`
}

func listVersions(t *testing.T, ts *httptest.Server, branch string) versionsResp {
	t.Helper()
	var body versionsResp
	path := "/api/versions?branch=" + url.QueryEscape(branch)
	if code := getJSON(t, ts, path, &body); code != http.StatusOK {
		t.Fatalf("GET %s = %d", path, code)
	}
	return body
}

// runOpOK starts an op, drains its stream and requires success.
func runOpOK(t *testing.T, ts *httptest.Server, body string) {
	t.Helper()
	events := readSSE(t, ts, startOpJSON(t, ts, body), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("op %s failed: %v", body, done)
	}
}

// A version is recorded by the operation that is about to overwrite it, so
// the fixture is a real merge run through the same transport the browser
// uses — not a hand-written ref.
func TestVersionsRecordedAndListed(t *testing.T) {
	dir := divergedRepo(t) // main and feature, one unique commit each
	beforeMerge := gitRun(t, dir, "rev-parse", "main")
	ts := serve(t, New(domain.Open(dir)))

	if got := listVersions(t, ts, "main"); len(got.Versions) != 0 {
		t.Fatalf("versions before any op = %+v, want none", got.Versions)
	}
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)

	got := listVersions(t, ts, "main")
	if len(got.Versions) != 1 {
		t.Fatalf("versions = %+v, want 1", got.Versions)
	}
	v := got.Versions[0]
	if v.Hash != beforeMerge {
		t.Errorf("hash = %s, want the pre-merge tip %s", v.Hash, beforeMerge)
	}
	if v.Op != "merge" {
		t.Errorf("op = %q, want merge", v.Op)
	}
	if !strings.HasPrefix(v.Ref, "refs/gg/versions/main/") {
		t.Errorf("ref = %q", v.Ref)
	}
	if v.Subject == "" || v.Unix == 0 || v.Short == "" {
		t.Errorf("row = %+v", v)
	}
	// An unrecorded branch is empty, not an error — that is also how a
	// deleted branch's history stays reachable.
	if got := listVersions(t, ts, "feature"); len(got.Versions) != 0 {
		t.Errorf("feature versions = %+v, want none", got.Versions)
	}
}

func TestRestoreVersion(t *testing.T) {
	dir := divergedRepo(t)
	beforeMerge := gitRun(t, dir, "rev-parse", "main")
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	if now := gitRun(t, dir, "rev-parse", "main"); now == beforeMerge {
		t.Fatal("merge did not move main — the fixture proves nothing")
	}
	ref := listVersions(t, ts, "main").Versions[0].Ref

	runOpOK(t, ts, fmt.Sprintf(`{"op":"restore-version","branch":"main","ref":%q}`, ref))
	if now := gitRun(t, dir, "rev-parse", "main"); now != beforeMerge {
		t.Errorf("main = %s, want the restored tip %s", now, beforeMerge)
	}
	// The restore snapshots what it overwrote, so it is itself undoable.
	after := listVersions(t, ts, "main").Versions
	if len(after) != 2 {
		t.Fatalf("versions after restore = %+v, want 2", after)
	}
	if after[0].Op != "restore" {
		t.Errorf("newest op = %q, want restore", after[0].Op)
	}
}

func TestDeleteVersion(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	ref := listVersions(t, ts, "main").Versions[0].Ref

	runOpOK(t, ts, fmt.Sprintf(`{"op":"delete-version","ref":%q}`, ref))
	if got := listVersions(t, ts, "main"); len(got.Versions) != 0 {
		t.Errorf("versions after delete = %+v, want none", got.Versions)
	}
}

// The op must refuse anything outside refs/gg/versions/ — a client bug must
// not be able to delete a real branch through it.
func TestDeleteVersionRefusesRealRef(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"delete-version","ref":"refs/heads/feature"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
	if out := gitRun(t, dir, "branch", "--list", "feature"); !strings.Contains(out, "feature") {
		t.Error("feature was deleted through delete-version")
	}
}

// A version ref of one branch must not be usable to move another.
func TestRestoreVersionRefusesCrossedBranch(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	ref := listVersions(t, ts, "main").Versions[0].Ref
	featureTip := gitRun(t, dir, "rev-parse", "feature")

	events := readSSE(t, ts, startOpJSON(t, ts,
		fmt.Sprintf(`{"op":"restore-version","branch":"feature","ref":%q}`, ref)), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
	if now := gitRun(t, dir, "rev-parse", "feature"); now != featureTip {
		t.Errorf("feature moved to %s despite the refusal", now)
	}
}

func TestVersionsRejects(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	if code := getJSON(t, ts, "/api/versions", nil); code != http.StatusBadRequest {
		t.Errorf("no branch = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/versions?branch="+url.QueryEscape("--all"), nil); code != http.StatusBadRequest {
		t.Errorf("leading dash = %d, want 400", code)
	}
	var out struct{}
	for _, body := range []string{
		`{"op":"restore-version","ref":"refs/gg/versions/main/1-merge"}`, // no branch
		`{"op":"restore-version","branch":"main"}`,                       // no ref
		`{"op":"restore-version","branch":"main","ref":"-x"}`,            // dash
		`{"op":"delete-version"}`,                                        // no ref
		`{"op":"delete-version","ref":"-x"}`,                             // dash
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", &out); code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, want 400", body, code)
		}
	}
}

// The all-branches picker read: every branch with recorded versions, the
// DELETED flag being the point — a deleted branch's snapshots (recorded by
// delete-branch itself) are only reachable through this listing.
func TestVersionBranches(t *testing.T) {
	dir := divergedRepo(t)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	var empty struct {
		Branches []struct{} `json:"branches"`
	}
	if code := getJSON(t, ts, "/api/version-branches", &empty); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(empty.Branches) != 0 {
		t.Fatalf("branches before any op = %v, want none", empty.Branches)
	}

	// merge records a snapshot on main; delete-branch records one on
	// feature and removes the branch (its confirm decision answered through
	// the transport — a raw `git branch -d` would record nothing).
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	waitDecision(t, srv.opByID(opID))
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	if done := readSSE(t, ts, opID, 30*time.Second); done[len(done)-1]["ok"] != true {
		t.Fatalf("delete-branch failed: %v", done[len(done)-1])
	}

	var body struct {
		Branches []struct {
			Branch     string `json:"branch"`
			Deleted    bool   `json:"deleted"`
			Count      int    `json:"count"`
			LatestUnix int64  `json:"latest_unix"`
		} `json:"branches"`
	}
	if code := getJSON(t, ts, "/api/version-branches", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	got := map[string][2]any{}
	for _, b := range body.Branches {
		if b.Count < 1 || b.LatestUnix == 0 {
			t.Errorf("%s: count=%d latest_unix=%d, want both set", b.Branch, b.Count, b.LatestUnix)
		}
		got[b.Branch] = [2]any{b.Deleted, b.Count}
	}
	if len(body.Branches) != 2 {
		t.Fatalf("branches = %v, want main + feature", got)
	}
	if v, ok := got["main"]; !ok || v[0] != false {
		t.Errorf("main = %v, want present and not deleted", v)
	}
	if v, ok := got["feature"]; !ok || v[0] != true {
		t.Errorf("feature = %v, want present and deleted", v)
	}
}
