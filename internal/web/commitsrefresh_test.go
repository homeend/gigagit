package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// feedRows GETs /api/commits (optionally the ?more=1 page) and returns the
// subjects of every row the server currently serves.
func feedRows(t *testing.T, ts *httptest.Server, more bool) []string {
	t.Helper()
	path := "/api/commits"
	if more {
		path += "?more=1"
	}
	var out struct {
		Rows []struct {
			Subject string `json:"subject"`
		} `json:"rows"`
	}
	if code := getJSON(t, ts, path, &out); code != http.StatusOK {
		t.Fatalf("GET %s: code = %d", path, code)
	}
	subs := make([]string, 0, len(out.Rows))
	for _, r := range out.Rows {
		subs = append(subs, r.Subject)
	}
	return subs
}

// A plain reload must not throw away the pages the browser scrolled in: the
// feed reconciles the fresh page 0 into what is already loaded.
func TestCommitsReloadKeepsPagedDepth(t *testing.T) {
	dir := newRepoDir(t, 30)
	srv := New(domain.Open(dir))
	srv.pageInitial, srv.pageBatch = 10, 10
	ts := serve(t, srv)

	if got := feedRows(t, ts, false); len(got) != 10 {
		t.Fatalf("first page = %d rows, want 10", len(got))
	}
	deep := feedRows(t, ts, true)
	if len(deep) != 20 {
		t.Fatalf("after ?more=1 = %d rows, want 20", len(deep))
	}

	gitRun(t, dir, "commit", "--allow-empty", "-m", "brand new")

	got := feedRows(t, ts, false)
	if len(got) != 21 {
		t.Fatalf("reload = %d rows, want 21 (20 kept + 1 new)", len(got))
	}
	if got[0] != "brand new" {
		t.Fatalf("head subject = %q, want the new commit", got[0])
	}
	for i, sub := range deep {
		if got[i+1] != sub {
			t.Fatalf("row %d = %q, want %q — the loaded tail changed", i+1, got[i+1], sub)
		}
	}
}

// The same must hold across a state-changing operation: committing from the
// browser adds a row on top, it does not collapse the list back to page 0.
func TestCommitsSurviveAnOp(t *testing.T) {
	dir := newRepoDir(t, 30)
	srv := New(domain.Open(dir))
	srv.pageInitial, srv.pageBatch = 10, 10
	ts := serve(t, srv)

	feedRows(t, ts, false)
	deep := feedRows(t, ts, true)
	if len(deep) != 20 {
		t.Fatalf("after ?more=1 = %d rows, want 20", len(deep))
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "new.txt")
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"commit","message":"from the browser"}`, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("commit start code = %d", code)
	}
	events := readSSE(t, ts, out.OpID, 20*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("commit op done = %v", done)
	}

	got := feedRows(t, ts, false)
	if len(got) != 21 {
		t.Fatalf("after the op = %d rows, want 21 (20 kept + the new commit)", len(got))
	}
	if got[0] != "from the browser" {
		t.Fatalf("head subject = %q, want the commit just made", got[0])
	}
}

// When history is rewritten past what one page can bridge, the reconcile finds
// no anchor and the list must fall back to a clean walk — never keep commits
// that no longer exist.
func TestCommitsReloadFallsBackAfterARewrite(t *testing.T) {
	dir := newRepoDir(t, 30)
	srv := New(domain.Open(dir))
	srv.pageInitial, srv.pageBatch = 10, 10
	ts := serve(t, srv)

	feedRows(t, ts, false)
	deep := feedRows(t, ts, true)
	if len(deep) != 20 {
		t.Fatalf("after ?more=1 = %d rows, want 20", len(deep))
	}

	// Drop far more commits than a page holds: nothing still loaded survives.
	gitRun(t, dir, "reset", "--hard", "HEAD~25")

	got := feedRows(t, ts, false)
	if len(got) != 5 {
		t.Fatalf("after the rewrite = %d rows, want the 5 surviving commits", len(got))
	}
	for _, sub := range got {
		for _, gone := range deep {
			if sub == gone {
				t.Fatalf("commit %q survived a rewrite that removed it", sub)
			}
		}
	}
}

// A MANUAL refresh (the ☰ row, r, the footer button) starts the list clean —
// the TUI's `r` with hardFeed. That is what makes it the escape hatch when a
// rewrite leaves a reconciled deep tail stale: the reconciling reload keeps
// the pages already scrolled in, which is exactly wrong there.
func TestCommitsResetStartsTheListClean(t *testing.T) {
	dir := newRepoDir(t, 12)
	srv := New(domain.Open(dir))
	srv.pageInitial, srv.pageBatch = 4, 4
	ts := serve(t, srv)

	if got := feedRows(t, ts, false); len(got) != 4 {
		t.Fatalf("first page = %d rows, want 4", len(got))
	}
	if got := feedRows(t, ts, true); len(got) != 8 {
		t.Fatalf("after ?more=1 = %d rows, want 8", len(got))
	}
	// the reconciling reload keeps that depth …
	if got := feedRows(t, ts, false); len(got) != 8 {
		t.Errorf("plain reload = %d rows, want the paged depth kept (8)", len(got))
	}
	// … and the manual one drops back to a single clean page
	var out struct {
		Rows []struct {
			Subject string `json:"subject"`
		} `json:"rows"`
	}
	if code := getJSON(t, ts, "/api/commits?reset=1", &out); code != http.StatusOK {
		t.Fatalf("reset reload: code = %d", code)
	}
	if len(out.Rows) != 4 {
		t.Errorf("reset reload = %d rows, want one clean page (4)", len(out.Rows))
	}
}
