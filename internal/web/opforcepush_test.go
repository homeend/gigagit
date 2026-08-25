package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// The force flag must not force anything by itself: it asks first. This is
// the whole posture — a client cannot express a silent force push.
func TestOpHTTPPushForceFlagAsksFirst(t *testing.T) {
	t.Parallel()
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	localCommit(t, clone, "l.txt", "l\n", "local work")
	remoteBefore := gitRun(t, origin, "rev-parse", "main")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startOpJSON(t, ts, `{"op":"push","branch":"main","force":true}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	// Straight to push-force — no rejection needed to get here — with the
	// safe mode first and an abort available.
	if req.ID != "push-force" {
		t.Fatalf("pending = %+v, want the push-force decision", req)
	}
	want := []string{"force-with-lease", "force", "abort"}
	if len(req.Options) != len(want) {
		t.Fatalf("options = %v, want %v", req.Options, want)
	}
	for i, o := range want {
		if req.Options[i] != o {
			t.Fatalf("options = %v, want %v", req.Options, want)
		}
	}
	// Aborting leaves the remote exactly where it was.
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v (abort is a clean no-change)", done)
	}
	if after := gitRun(t, origin, "rev-parse", "main"); after != remoteBefore {
		t.Errorf("origin moved on abort: %s -> %s", remoteBefore, after)
	}
	if subjects := gitRun(t, origin, "log", "--format=%s"); !strings.Contains(subjects, "remote work") {
		t.Errorf("remote commit lost despite abort:\n%s", subjects)
	}
}

// And when the answer IS force, it overwrites — the flag reaches the real op,
// not a dead end.
func TestOpHTTPPushForceFlagThenForce(t *testing.T) {
	t.Parallel()
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	localCommit(t, clone, "l.txt", "l\n", "local work")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startOpJSON(t, ts, `{"op":"push","branch":"main","force":true}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"force"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
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

// An ordinary push must be unaffected: no flag, no force prompt.
func TestOpHTTPPushWithoutForceFlagStillPlain(t *testing.T) {
	t.Parallel()
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	localCommit(t, clone, "l.txt", "l\n", "local work")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startOpJSON(t, ts, `{"op":"push","branch":"main"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "push-rejected" {
		t.Fatalf("pending = %+v, want push-rejected (the flag-less path is unchanged)", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"abort"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	readSSE(t, ts, opID, 30*time.Second)
	if subjects := gitRun(t, origin, "log", "--format=%s"); !strings.Contains(subjects, "remote work") {
		t.Errorf("remote commit lost:\n%s", subjects)
	}
}
