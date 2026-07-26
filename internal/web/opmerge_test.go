package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// divergedRepo builds main and feature with one unique commit each, leaving
// main checked out — a non-fast-forward merge and a real rebase both need it.
func divergedRepo(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "feature work")
	gitRun(t, dir, "checkout", "main")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "main work")
	return dir
}

func TestOpHTTPMerge(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if log := gitRun(t, dir, "log", "--oneline", "main"); !strings.Contains(log, "feature work") {
		t.Errorf("feature work not merged into main:\n%s", log)
	}
	// SmartMerge ends on the target.
	if head := strings.TrimSpace(gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); head != "main" {
		t.Errorf("HEAD = %q, want main", head)
	}
}

// Target need not be checked out: SmartMerge's ladder switches to it and
// ends there. This is what makes an arbitrary drop pair work.
func TestOpHTTPMergeTargetNotCheckedOut(t *testing.T) {
	dir := divergedRepo(t)
	gitRun(t, dir, "checkout", "feature") // main is now the non-checked-out target
	ts := serve(t, New(domain.Open(dir)))

	opID := startOpBody(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if log := gitRun(t, dir, "log", "--oneline", "main"); !strings.Contains(log, "feature work") {
		t.Errorf("feature work not merged into main:\n%s", log)
	}
}

func TestOpHTTPMergeSameBranch(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"merge","branch":"main","onto":"main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (source == target)", done)
	}
}

func TestOpHTTPMergeBadNames(t *testing.T) {
	dir := divergedRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"merge","onto":"main"}`,
		`{"op":"merge","branch":"feature"}`,
		`{"op":"merge","branch":"--exec=id","onto":"main"}`,
		`{"op":"merge","branch":"feature","onto":"--exec=id"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
