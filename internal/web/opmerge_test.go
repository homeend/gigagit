package web

import (
	"net/http"
	"os"
	"path/filepath"
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

// conflictingRepo builds main and feature that both edit the SAME line of
// f.txt, guaranteeing a real merge conflict — divergedRepo's commits are
// --allow-empty (touch no file at all) so they can never conflict.
func conflictingRepo(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "feature edit")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main edit")
	return dir
}

// TestOpHTTPMergeConflict drives a conflicted merge through the drop path's
// exact wire contract: POST /api/op, park on the merge-conflict decision,
// answer keep-conflicts, then verify the conflict is really left in the
// tree (git-visible, not just an HTTP response shape).
func TestOpHTTPMergeConflict(t *testing.T) {
	dir := conflictingRepo(t)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "merge-conflict" {
		t.Fatalf("pending = %+v, want id merge-conflict", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"keep-conflicts"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false || done["changed"] != true {
		t.Fatalf("done = %v, want ok=false changed=true (keep-conflicts)", done)
	}
	// git-visible conflict state, not just the wire response shape.
	if out := gitRun(t, dir, "ls-files", "-u"); out == "" {
		t.Error("expected unmerged index entries after keep-conflicts, got none")
	}
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if i, ok := findFile(t, st, "f.txt"); !ok || st.Files[i].Kind != "conflicted" {
		t.Errorf("f.txt not conflicted after keep-conflicts: %+v", st.Files)
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
