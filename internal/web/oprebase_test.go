package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPRebase(t *testing.T) {
	dir := divergedRepo(t) // main checked out; feature diverged
	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"rebase","branch":"feature","onto":"main"}`)
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	// feature now sits on top of main's tip.
	if log := gitRun(t, dir, "log", "--oneline", "feature"); !strings.Contains(log, "main work") {
		t.Errorf("feature not rebased onto main:\n%s", log)
	}
	// SmartRebase pivots on the moving branch and ends there.
	if head := strings.TrimSpace(gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); head != "feature" {
		t.Errorf("HEAD = %q, want feature", head)
	}
}

func TestOpHTTPRebaseSameBranch(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"rebase","branch":"main","onto":"main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (branch == base)", done)
	}
}

func TestOpHTTPRebaseBadNames(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"rebase","onto":"main"}`,
		`{"op":"rebase","branch":"feature"}`,
		`{"op":"rebase","branch":"--exec=id","onto":"main"}`,
		`{"op":"rebase","branch":"feature","onto":"--exec=id"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
