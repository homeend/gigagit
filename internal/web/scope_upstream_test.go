package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// upstreamRepo: a clone whose origin moved on after the clone (fetched, not
// pulled), so origin/main holds a commit local main does not; a local
// feature branch with no upstream; and an origin topic branch with a commit
// of its own, fetched, with a local topic that does not track it yet.
func upstreamRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	up := filepath.Join(root, "up")
	gitRun(t, root, "init", "-b", "main", up)
	gitRun(t, up, "config", "user.email", "t@example.com")
	gitRun(t, up, "config", "user.name", "t")
	gitRun(t, up, "commit", "--allow-empty", "-m", "base")
	local := filepath.Join(root, "local")
	gitRun(t, root, "clone", "-q", up, local)
	gitRun(t, local, "config", "user.email", "t@example.com")
	gitRun(t, local, "config", "user.name", "t")
	gitRun(t, up, "commit", "--allow-empty", "-m", "ahead on origin")
	gitRun(t, up, "checkout", "-b", "topic", "HEAD~1")
	gitRun(t, up, "commit", "--allow-empty", "-m", "origin topic")
	gitRun(t, up, "checkout", "main")
	gitRun(t, local, "fetch", "-q", "origin")
	gitRun(t, local, "checkout", "-q", "-b", "feature")
	gitRun(t, local, "commit", "--allow-empty", "-m", "only on feature")
	gitRun(t, local, "branch", "--no-track", "topic", "main")
	gitRun(t, local, "checkout", "-q", "main")
	return local
}


// The TUI walks every in-scope local branch's upstream too (feedUpstreams):
// a branch's remote tip shows even while the local branch is behind it.
func TestFeedWalksUpstreams(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(upstreamRepo(t))))

	if !has(readScope(t, ts).subjects(), "ahead on origin") {
		t.Errorf("unscoped feed misses origin/main's commit: %v", readScope(t, ts).subjects())
	}
	if code := setSoloHTTP(t, ts, `{"branch":"feature"}`); code != http.StatusOK {
		t.Fatalf("solo feature code = %d", code)
	}
	if subs := readScope(t, ts).subjects(); has(subs, "ahead on origin") {
		t.Errorf("feature (no upstream) walks origin/main: %v", subs)
	}
	if code := setSoloHTTP(t, ts, `{"branch":"main"}`); code != http.StatusOK {
		t.Fatalf("solo main code = %d", code)
	}
	if subs := readScope(t, ts).subjects(); !has(subs, "ahead on origin") {
		t.Errorf("main's scope misses its upstream's commit: %v", subs)
	}
}

// The upstream list is read per request: a branch that gains an upstream
// re-walks the feed on the next plain request — the feed is not reset after
// an op, so a list fixed when the feed was built would never learn it.
func TestFeedUpstreamChangeRewalks(t *testing.T) {
	t.Parallel()
	dir := upstreamRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	if code := setSoloHTTP(t, ts, `{"branch":"topic"}`); code != http.StatusOK {
		t.Fatalf("solo topic code = %d", code)
	}
	if subs := readScope(t, ts).subjects(); has(subs, "origin topic") {
		t.Fatalf("untracked topic already walks origin/topic: %v", subs)
	}
	gitRun(t, dir, "branch", "-u", "origin/topic", "topic")
	if subs := readScope(t, ts).subjects(); !has(subs, "origin topic") {
		t.Errorf("topic's new upstream not walked: %v", subs)
	}
}
