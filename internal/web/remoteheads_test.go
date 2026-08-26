package web

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// containsLine reports whether text has a line exactly equal to want.
func containsLine(text, want string) bool {
	for _, ln := range strings.Split(text, "\n") {
		if strings.TrimSpace(ln) == want {
			return true
		}
	}
	return false
}

// narrowClone builds a bare origin carrying main plus a "hidden" branch, and
// a --single-branch clone whose fetch refspec covers only main — so "hidden"
// is an unfetched remote head, the browse-remote-branches case.
func narrowClone(t *testing.T) (origin, clone string) {
	t.Helper()
	origin, _ = cloneWithOrigin(t)
	base := t.TempDir()
	work := filepath.Join(base, "w")
	gitRun(t, base, "clone", origin, work)
	gitRun(t, work, "branch", "hidden", "main")
	gitRun(t, work, "push", "origin", "hidden")
	clone = filepath.Join(base, "narrow")
	gitRun(t, base, "clone", "--single-branch", origin, clone)
	gitRun(t, clone, "config", "user.email", "t@example.com")
	gitRun(t, clone, "config", "user.name", "t")
	return origin, clone
}

// GET /api/remote-heads with no remote lists the configured remote names.
func TestRemoteHeadsListsRemotes(t *testing.T) {
	t.Parallel()
	_, clone := narrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	var out struct {
		Remotes []string `json:"remotes"`
	}
	if code := getJSON(t, ts, "/api/remote-heads", &out); code != 200 {
		t.Fatalf("status = %d", code)
	}
	if len(out.Remotes) != 1 || out.Remotes[0] != "origin" {
		t.Fatalf("remotes = %v, want [origin]", out.Remotes)
	}
}

// GET /api/remote-heads?remote=origin lists only the unfetched heads.
func TestRemoteHeadsListsUnfetched(t *testing.T) {
	t.Parallel()
	_, clone := narrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	var out struct {
		Remote string `json:"remote"`
		Heads  []struct {
			Name string `json:"name"`
			Hash string `json:"hash"`
		} `json:"heads"`
	}
	if code := getJSON(t, ts, "/api/remote-heads?remote=origin", &out); code != 200 {
		t.Fatalf("status = %d", code)
	}
	if out.Remote != "origin" || len(out.Heads) != 1 || out.Heads[0].Name != "hidden" {
		t.Fatalf("out = %+v, want the single unfetched head 'hidden'", out)
	}
	if len(out.Heads[0].Hash) != 40 {
		t.Fatalf("hash = %q, want full sha", out.Heads[0].Hash)
	}
}

// An unknown or unsafe remote is refused before any network read.
func TestRemoteHeadsRefusals(t *testing.T) {
	t.Parallel()
	_, clone := narrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	if code := getJSON(t, ts, "/api/remote-heads?remote=nosuch", nil); code != http.StatusNotFound {
		t.Fatalf("unknown remote: status = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/remote-heads?remote=--upload-pack=x", nil); code != http.StatusBadRequest {
		t.Fatalf("unsafe remote: status = %d, want 400", code)
	}
}

// checkout-remote-head with stay intent materializes the branch and mapping
// without moving HEAD.
func TestOpCheckoutRemoteHeadStay(t *testing.T) {
	t.Parallel()
	_, clone := narrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout-remote-head","remote":"origin","branch":"hidden"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if cur := gitRun(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); cur != "main" {
		t.Errorf("current = %q, want main (stay intent)", cur)
	}
	if at, want := gitRun(t, clone, "rev-parse", "hidden"), gitRun(t, clone, "rev-parse", "origin/hidden"); at != want {
		t.Errorf("hidden = %s, want %s", at, want)
	}
	if up := gitRun(t, clone, "rev-parse", "--abbrev-ref", "hidden@{upstream}"); up != "origin/hidden" {
		t.Errorf("upstream = %q", up)
	}
	if specs := gitRun(t, clone, "config", "--get-all", "remote.origin.fetch"); !containsLine(specs, "+refs/heads/hidden:refs/remotes/origin/hidden") {
		t.Errorf("fetch specs = %q, want the per-branch mapping", specs)
	}
}

// switch intent lands on the new branch.
func TestOpCheckoutRemoteHeadSwitch(t *testing.T) {
	t.Parallel()
	_, clone := narrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout-remote-head","remote":"origin","branch":"hidden","switch":true}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if cur := gitRun(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); cur != "hidden" {
		t.Errorf("current = %q, want hidden (switch intent)", cur)
	}
}

// Wire values are identifiers resolved server-side: missing/unsafe params,
// an unknown remote or branch, and a branch that is already tracked are all
// refused before any op starts.
func TestOpCheckoutRemoteHeadRefusals(t *testing.T) {
	t.Parallel()
	_, clone := narrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	cases := []struct {
		body string
		want int
	}{
		{`{"op":"checkout-remote-head","branch":"hidden"}`, http.StatusBadRequest},
		{`{"op":"checkout-remote-head","remote":"origin"}`, http.StatusBadRequest},
		{`{"op":"checkout-remote-head","remote":"-x","branch":"hidden"}`, http.StatusBadRequest},
		{`{"op":"checkout-remote-head","remote":"origin","branch":"-x"}`, http.StatusBadRequest},
		{`{"op":"checkout-remote-head","remote":"nosuch","branch":"hidden"}`, http.StatusNotFound},
		{`{"op":"checkout-remote-head","remote":"origin","branch":"nosuch"}`, http.StatusNotFound},
		{`{"op":"checkout-remote-head","remote":"origin","branch":"main"}`, http.StatusNotFound}, // tracked → not an unfetched head
	}
	for _, c := range cases {
		var out map[string]any
		if code := postJSON(t, ts, "/api/op", c.body, "application/json", ts.URL, &out); code != c.want {
			t.Errorf("%s: status = %d, want %d (%v)", c.body, code, c.want, out)
		}
	}
}
