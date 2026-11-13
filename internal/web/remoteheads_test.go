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

// Wire values are identifiers resolved server-side: missing/unsafe params and
// an unknown remote or branch are refused before any op starts.
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
	}
	for _, c := range cases {
		var out map[string]any
		if code := postJSON(t, ts, "/api/op", c.body, "application/json", ts.URL, &out); code != c.want {
			t.Errorf("%s: status = %d, want %d (%v)", c.body, code, c.want, out)
		}
	}
}

// wideClone is the ordinary full clone: origin carries main plus "side", and
// the clone's wildcard refspec tracks both — every remote branch is tracked.
func wideClone(t *testing.T) (origin, clone string) {
	t.Helper()
	origin, _ = cloneWithOrigin(t)
	base := t.TempDir()
	work := filepath.Join(base, "w")
	gitRun(t, base, "clone", origin, work)
	gitRun(t, work, "branch", "side", "main")
	gitRun(t, work, "push", "origin", "side")
	clone = filepath.Join(base, "wide")
	gitRun(t, base, "clone", origin, clone)
	gitRun(t, clone, "config", "user.email", "t@example.com")
	gitRun(t, clone, "config", "user.name", "t")
	return origin, clone
}

type remoteHeadsBody struct {
	Remote string `json:"remote"`
	Heads  []struct {
		Name    string `json:"name"`
		Hash    string `json:"hash"`
		Tracked bool   `json:"tracked"`
	} `json:"heads"`
	Error string `json:"error"`
}

func (b remoteHeadsBody) tracked() map[string]bool {
	out := map[string]bool{}
	for _, h := range b.Heads {
		out[h.Name] = h.Tracked
	}
	return out
}

// The picker lists EVERY branch on the remote — the tracked ones too, flagged —
// so a pasted name that exists always matches (user report: with a full
// refspec the picker answered "every branch is already tracked" and the
// branch could not be checked out from it).
func TestRemoteHeadsListsTrackedToo(t *testing.T) {
	t.Parallel()
	_, clone := narrowClone(t)
	ts := serve(t, New(domain.Open(clone)))
	var out remoteHeadsBody
	if code := getJSON(t, ts, "/api/remote-heads?remote=origin", &out); code != 200 {
		t.Fatalf("status = %d", code)
	}
	got := out.tracked()
	if tr, ok := got["main"]; !ok || !tr {
		t.Errorf("main: listed=%v tracked=%v, want listed and tracked (%+v)", ok, tr, out)
	}
	if tr, ok := got["hidden"]; !ok || tr {
		t.Errorf("hidden: listed=%v tracked=%v, want listed and not tracked (%+v)", ok, tr, out)
	}
	for _, h := range out.Heads {
		if len(h.Hash) != 40 {
			t.Errorf("%s: hash %q, want the remote's full sha", h.Name, h.Hash)
		}
	}
}

// An unreachable remote still lists what is tracked locally, with the error
// said beside it — the tracked branches need no network to check out.
func TestRemoteHeadsUnreachableKeepsTracked(t *testing.T) {
	t.Parallel()
	_, clone := wideClone(t)
	gitRun(t, clone, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	ts := serve(t, New(domain.Open(clone)))
	var out remoteHeadsBody
	if code := getJSON(t, ts, "/api/remote-heads?remote=origin", &out); code != 200 {
		t.Fatalf("status = %d", code)
	}
	if out.Error == "" {
		t.Error("an unreachable remote must say so")
	}
	if got := out.tracked(); !got["side"] || !got["main"] {
		t.Errorf("tracked rows = %v, want main and side", got)
	}
}

// A TRACKED branch checks out from its remote-tracking ref (the Remotes
// menu's check-out), never through the per-branch mapping path: a wildcard
// refspec must not gain a redundant fetch line.
func TestOpCheckoutRemoteHeadTracked(t *testing.T) {
	t.Parallel()
	_, clone := wideClone(t)
	ts := serve(t, New(domain.Open(clone)))
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout-remote-head","remote":"origin","branch":"side","switch":true}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if cur := gitRun(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); cur != "side" {
		t.Errorf("current = %q, want side", cur)
	}
	if up := gitRun(t, clone, "rev-parse", "--abbrev-ref", "side@{upstream}"); up != "origin/side" {
		t.Errorf("upstream = %q", up)
	}
	if specs := gitRun(t, clone, "config", "--get-all", "remote.origin.fetch"); strings.Contains(specs, "refs/heads/side") {
		t.Errorf("fetch specs = %q, want no per-branch mapping for a tracked branch", specs)
	}
}
