package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// One branch strictly behind the other: the probe names the direction, in
// either argument order.
func TestFFPairBehindAhead(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	gitRun(t, dir, "branch", "old", "HEAD~2")
	ts := serve(t, New(domain.Open(dir)))

	for _, q := range []string{"?a=old&b=main", "?a=main&b=old"} {
		var raw map[string]any
		if code := getJSON(t, ts, "/api/ff-pair"+q, &raw); code != http.StatusOK {
			t.Fatalf("%s: code = %d", q, code)
		}
		if raw["ok"] != true || raw["behind"] != "old" || raw["ahead"] != "main" {
			t.Fatalf("%s: %v, want ok behind=old ahead=main", q, raw)
		}
	}
}

// Equal tips report ok=false; unknown names are a 404 (allowlist precedent).
func TestFFPairNoneAndUnknown(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	gitRun(t, dir, "branch", "twin")
	ts := serve(t, New(domain.Open(dir)))

	var raw map[string]any
	if code := getJSON(t, ts, "/api/ff-pair?a=twin&b=main", &raw); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if raw["ok"] != false {
		t.Fatalf("equal tips: %v, want ok=false", raw)
	}
	if code := getJSON(t, ts, "/api/ff-pair?a=ghost&b=main", &raw); code != http.StatusNotFound {
		t.Fatalf("unknown branch: code = %d, want 404", code)
	}
}

// The pair lane: branch+onto advances the named (non-current) branch.
func TestOpHTTPFastForwardPair(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	tip := gitRun(t, dir, "rev-parse", "main")
	gitRun(t, dir, "branch", "behind", "HEAD~2")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"fast-forward","branch":"behind","onto":"main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "behind"); got != tip {
		t.Errorf("behind = %s, want fast-forwarded to %s", got, tip)
	}
	if cur := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); cur != "main" {
		t.Errorf("current branch = %q, want main (pair ff must not switch)", cur)
	}
}

// An unknown onto is rejected before any git runs.
func TestOpHTTPFastForwardPairUnknownOnto(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	gitRun(t, dir, "branch", "behind", "HEAD~2")
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", `{"op":"fast-forward","branch":"behind","onto":"ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", code)
	}
}
