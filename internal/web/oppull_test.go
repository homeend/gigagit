package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// cloneWithOrigin builds a bare origin (one commit on main) plus a working
// clone whose main tracks origin/main — a real remote with zero network.
func cloneWithOrigin(t *testing.T) (origin, clone string) {
	t.Helper()
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	gitRun(t, base, "init", "-b", "main", seed)
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, seed, "add", "-A")
	gitRun(t, seed, "commit", "-m", "base")
	origin = filepath.Join(base, "origin.git")
	gitRun(t, base, "clone", "--bare", seed, origin)
	clone = filepath.Join(base, "clone")
	gitRun(t, base, "clone", origin, clone)
	gitRun(t, clone, "config", "user.email", "t@example.com")
	gitRun(t, clone, "config", "user.name", "t")
	return origin, clone
}

// pushRemoteCommit adds a commit to origin/main via a throwaway second clone.
func pushRemoteCommit(t *testing.T, origin, file, content, msg string) {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "w")
	gitRun(t, dir, "clone", origin, work)
	if err := os.WriteFile(filepath.Join(work, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "add", "-A")
	gitRun(t, work, "commit", "-m", msg)
	gitRun(t, work, "push", "origin", "main")
}

func startPull(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"pull"}`, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("pull start code = %d", code)
	}
	return out.OpID
}

func TestOpHTTPPullFastForward(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startPull(t, ts), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "pulled") {
		t.Errorf("summary = %v", done["summary"])
	}
	local := gitRun(t, clone, "rev-parse", "main")
	remote := gitRun(t, clone, "rev-parse", "origin/main")
	if local != remote {
		t.Errorf("local %s != origin %s after ff pull", local, remote)
	}
}

func TestOpHTTPPullDivergedRebase(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	if err := os.WriteFile(filepath.Join(clone, "l.txt"), []byte("l\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "add", "-A")
	gitRun(t, clone, "commit", "-m", "local work")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startPull(t, ts)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "non-fast-forward" || len(req.Options) != 4 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"rebase"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	// rebase linearizes: local work sits atop remote work
	subjects := gitRun(t, clone, "log", "--format=%s", "-3")
	if !strings.HasPrefix(subjects, "local work\nremote work") {
		t.Errorf("log after rebase:\n%s", subjects)
	}
}

func TestOpHTTPPullDivergedAbort(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	if err := os.WriteFile(filepath.Join(clone, "l.txt"), []byte("l\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "add", "-A")
	gitRun(t, clone, "commit", "-m", "local work")
	before := gitRun(t, clone, "rev-parse", "main")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startPull(t, ts)
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
	if !strings.Contains(done["summary"].(string), "aborted") {
		t.Errorf("summary = %v", done["summary"])
	}
	if after := gitRun(t, clone, "rev-parse", "main"); after != before {
		t.Errorf("tip moved on abort: %s -> %s", before, after)
	}
}

func TestOpHTTPPullConflictedRebase(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "f.txt", "remote\n", "remote edit")
	if err := os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "add", "-A")
	gitRun(t, clone, "commit", "-m", "local edit")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startPull(t, ts)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"rebase"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (conflicted rebase)", done)
	}
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if i, ok := findFile(t, st, "f.txt"); !ok || st.Files[i].Kind != "conflicted" {
		t.Errorf("f.txt not conflicted after rebase conflict: %+v", st.Files)
	}
}

func TestOpHTTPPullNoRemote(t *testing.T) {
	dir := newRepoDir(t, 1) // no origin at all
	ts := serve(t, New(domain.Open(dir)))
	events := readSSE(t, ts, startPull(t, ts), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
	if done["error"] == nil || done["error"] == "" {
		t.Error("missing error detail")
	}
}
