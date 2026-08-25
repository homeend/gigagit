package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// startOpBody starts any op from a raw JSON body and returns the op id.
func startOpBody(t *testing.T, ts *httptest.Server, body string) string {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", body, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("op start code = %d (body %s)", code, body)
	}
	return out.OpID
}

func TestOpHTTPDeleteBranchMerged(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "branch", "feature") // at HEAD: fully merged
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "delete-branch" || len(req.Options) != 2 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "deleted branch feature") {
		t.Errorf("summary = %v", done["summary"])
	}
	if out := gitRun(t, dir, "branch", "--list", "feature"); strings.TrimSpace(out) != "" {
		t.Errorf("branch still listed: %q", out)
	}
}

func TestOpHTTPDeleteBranchConfirmAbort(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "branch", "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v (abort is a clean no-change)", done)
	}
	if strings.TrimSpace(gitRun(t, dir, "branch", "--list", "feature")) == "" {
		t.Error("branch gone after abort")
	}
}

func TestOpHTTPDeleteBranchUnmergedKeep(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "unmerged work")
	gitRun(t, dir, "checkout", "main")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide delete code = %d", code)
	}
	waitDecision(t, run) // the unmerged fork parks next
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "branch-unmerged" || len(req.Options) != 2 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"keep"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide keep code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v", done)
	}
	if strings.TrimSpace(gitRun(t, dir, "branch", "--list", "feature")) == "" {
		t.Error("branch gone after keep")
	}
}

func TestOpHTTPDeleteBranchUnmergedForce(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "unmerged work")
	gitRun(t, dir, "checkout", "main")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide delete code = %d", code)
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"force-delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide force-delete code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "branch", "--list", "feature"); strings.TrimSpace(out) != "" {
		t.Errorf("branch still listed after force-delete: %q", out)
	}
}

func TestOpHTTPDeleteBranchCurrent(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"delete-branch","branch":"main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (checked-out branch)", done)
	}
	if err, _ := done["error"].(string); !strings.Contains(err, "checked-out branch") {
		t.Errorf("error = %v", done["error"])
	}
}

func TestOpHTTPDeleteBranchInWorktree(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "branch", "feature")
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, dir, "worktree", "add", wt, "feature")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (branch in worktree)", done)
	}
	if err, _ := done["error"].(string); !strings.Contains(err, "worktree") {
		t.Errorf("error = %v", done["error"])
	}
}

func TestOpHTTPDeleteBranchBadName(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"delete-branch"}`,
		`{"op":"delete-branch","branch":"--delete"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
