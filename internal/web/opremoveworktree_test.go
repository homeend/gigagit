package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// addWorktree creates <branch> at HEAD and checks it out in a fresh linked
// worktree, returning the worktree path.
func addWorktree(t *testing.T, dir, branch string) string {
	t.Helper()
	gitRun(t, dir, "branch", branch)
	wt := filepath.Join(t.TempDir(), "wt-"+branch)
	gitRun(t, dir, "worktree", "add", wt, branch)
	return wt
}

func removeWtBody(path string) string {
	return fmt.Sprintf(`{"op":"remove-worktree","path":%q}`, path)
}

func TestOpHTTPRemoveWorktreeOnly(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, removeWtBody(wt))
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "remove-scope" || len(req.Options) != 3 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"worktree-only"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists (stat err %v)", err)
	}
	if strings.TrimSpace(gitRun(t, dir, "branch", "--list", "feature")) == "" {
		t.Error("worktree-only removed the branch too")
	}
}

func TestOpHTTPRemoveWorktreeAbort(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, removeWtBody(wt))
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree dir gone after abort: %v", err)
	}
}

func TestOpHTTPRemoveWorktreeAndBranch(t *testing.T) {
	dir := newRepoDir(t, 1)
	wt := addWorktree(t, dir, "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, removeWtBody(wt))
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"worktree-and-branch"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists (stat err %v)", err)
	}
	if out := gitRun(t, dir, "branch", "--list", "feature"); strings.TrimSpace(out) != "" {
		t.Errorf("branch still listed: %q", out)
	}
}

func TestOpHTTPRemoveWorktreeUnknownPath(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", removeWtBody("/nonexistent/wt"), "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("unknown path code = %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"remove-worktree","path":""}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("empty path code = %d, want 400", code)
	}
}

func TestOpHTTPRemoveWorktreeMain(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	// The main worktree IS in the server's list, so the allowlist passes and
	// the ENGINE guard must refuse it.
	events := readSSE(t, ts, startOpBody(t, ts, removeWtBody(dir)), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (main worktree)", done)
	}
	if done["error"] == nil || done["error"] == "" {
		t.Error("missing error detail")
	}
}
