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

func startPush(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"push"}`, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("push start code = %d", code)
	}
	return out.OpID
}

// localCommit adds a commit in the clone so it is ahead of (or diverged from)
// origin.
func localCommit(t *testing.T, clone, file, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(clone, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "add", "-A")
	gitRun(t, clone, "commit", "-m", msg)
}

func TestOpHTTPPushClean(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	localCommit(t, clone, "l.txt", "l\n", "local work")
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startPush(t, ts), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "pushed") {
		t.Errorf("summary = %v", done["summary"])
	}
	if local, remote := gitRun(t, clone, "rev-parse", "main"), gitRun(t, origin, "rev-parse", "main"); local != remote {
		t.Errorf("origin %s != local %s after push", remote, local)
	}
}

func TestOpHTTPPushRejectedAbort(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	localCommit(t, clone, "l.txt", "l\n", "local work")
	remoteBefore := gitRun(t, origin, "rev-parse", "main")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startPush(t, ts)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	// current branch is the pushed branch, so rebase is offered too
	if req.ID != "push-rejected" || len(req.Options) != 3 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v (abort is a clean no-change)", done)
	}
	if !strings.Contains(done["summary"].(string), "cancelled") {
		t.Errorf("summary = %v", done["summary"])
	}
	if after := gitRun(t, origin, "rev-parse", "main"); after != remoteBefore {
		t.Errorf("origin tip moved on abort: %s -> %s", remoteBefore, after)
	}
}

func TestOpHTTPPushRejectedRebase(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	localCommit(t, clone, "l.txt", "l\n", "local work")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startPush(t, ts)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"rebase"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	// rebase linearized local atop remote, then pushed: origin has both
	subjects := gitRun(t, origin, "log", "--format=%s", "-3")
	if !strings.HasPrefix(subjects, "local work\nremote work") {
		t.Errorf("origin log after rebase+push:\n%s", subjects)
	}
	if local, remote := gitRun(t, clone, "rev-parse", "main"), gitRun(t, origin, "rev-parse", "main"); local != remote {
		t.Errorf("origin %s != local %s after rebase+push", remote, local)
	}
}

func TestOpHTTPPushRejectedForcePlain(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	localCommit(t, clone, "l.txt", "l\n", "local work")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startPush(t, ts)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"force"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide push-rejected code = %d", code)
	}
	waitDecision(t, run) // the chained push-force confirm parks next
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "push-force" || len(req.Options) != 3 {
		t.Fatalf("pending = %+v", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"force"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide push-force code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if local, remote := gitRun(t, clone, "rev-parse", "main"), gitRun(t, origin, "rev-parse", "main"); local != remote {
		t.Errorf("origin %s != local %s after force push", remote, local)
	}
	if subjects := gitRun(t, origin, "log", "--format=%s"); strings.Contains(subjects, "remote work") {
		t.Errorf("remote commit survived a plain force push:\n%s", subjects)
	}
}

func TestOpHTTPPushForceWithLeaseRefusedStale(t *testing.T) {
	// A failed push never updates the remote-tracking ref, so the lease is
	// stale and --force-with-lease REFUSES — the safety property, not a bug.
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	localCommit(t, clone, "l.txt", "l\n", "local work")
	remoteBefore := gitRun(t, origin, "rev-parse", "main")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startPush(t, ts)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"force"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide push-rejected code = %d", code)
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"force-with-lease"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide push-force code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (stale lease must refuse)", done)
	}
	if done["error"] == nil || done["error"] == "" {
		t.Error("missing error detail")
	}
	if after := gitRun(t, origin, "rev-parse", "main"); after != remoteBefore {
		t.Errorf("origin tip moved despite stale lease: %s -> %s", remoteBefore, after)
	}
}

func TestOpHTTPPushDetachedHead(t *testing.T) {
	_, clone := cloneWithOrigin(t)
	gitRun(t, clone, "checkout", "--detach")
	ts := serve(t, New(domain.Open(clone)))

	code := postJSON(t, ts, "/api/op", `{"op":"push"}`, "application/json", "", nil)
	if code != http.StatusConflict {
		t.Fatalf("detached-HEAD push code = %d, want 409", code)
	}
}
