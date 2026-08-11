package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPCheckoutDetached(t *testing.T) {
	dir := newRepoDir(t, 2)
	prev := gitRun(t, dir, "rev-parse", "HEAD~1")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout","sha":"`+prev+`"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if ref := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); ref != "HEAD" {
		t.Errorf("HEAD = %q, want detached", ref)
	}
	if at := gitRun(t, dir, "rev-parse", "HEAD"); at != prev {
		t.Errorf("HEAD at %s, want %s", at, prev)
	}
}

func TestOpHTTPCheckoutNewBranch(t *testing.T) {
	dir := newRepoDir(t, 2)
	prev := gitRun(t, dir, "rev-parse", "HEAD~1")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout","sha":"`+prev+`","name":"rescue"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if ref := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); ref != "rescue" {
		t.Errorf("HEAD = %q, want rescue", ref)
	}
	if at := gitRun(t, dir, "rev-parse", "HEAD"); at != prev {
		t.Errorf("HEAD at %s, want %s", at, prev)
	}
}

// The checkout target is hex-only: a commit id is content-addressed, so there
// is no stale-identifier hazard — but names (branches, "HEAD~1") have their
// own dedicated ops and are refused here.
func TestOpHTTPCheckoutBadInput(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"checkout"}`,
		`{"op":"checkout","sha":"main"}`,
		`{"op":"checkout","sha":"HEAD~1"}`,
		`{"op":"checkout","sha":"abc"}`,
		`{"op":"checkout","sha":"-deadbeefcafe"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}

func TestOpHTTPCheckoutBadName(t *testing.T) {
	dir := newRepoDir(t, 1)
	head := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", `{"op":"checkout","sha":"`+head+`","name":"-x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}

// Empty mode keeps the engine's interactive flow: the soft/mixed/hard picker
// parks in the browser modal and is itself the deliberate confirmation.
func TestOpHTTPResetInteractive(t *testing.T) {
	dir := newRepoDir(t, 3)
	target := gitRun(t, dir, "rev-parse", "HEAD~1")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpJSON(t, ts, `{"op":"reset","sha":"`+target+`"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "reset-mode" {
		t.Fatalf("pending = %+v, want reset-mode", req)
	}
	want := []string{"soft", "mixed", "hard", "cancel"}
	for i, o := range want {
		if i >= len(req.Options) || req.Options[i] != o {
			t.Fatalf("options = %v, want %v", req.Options, want)
		}
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"mixed"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if at := gitRun(t, dir, "rev-parse", "HEAD"); at != target {
		t.Errorf("HEAD at %s, want %s", at, target)
	}
	// mixed keeps the working tree: f.txt still holds c3's content, unstaged.
	// (gitRun trims, so unstaged " M f.txt" arrives as exactly "M f.txt";
	// staged would keep its two inner spaces.)
	if st := gitRun(t, dir, "status", "--porcelain"); st != "M f.txt" {
		t.Errorf("status = %q, want f.txt modified-unstaged", st)
	}
}

func TestOpHTTPResetCancel(t *testing.T) {
	dir := newRepoDir(t, 2)
	before := gitRun(t, dir, "rev-parse", "HEAD")
	target := gitRun(t, dir, "rev-parse", "HEAD~1")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpJSON(t, ts, `{"op":"reset","sha":"`+target+`"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"cancel"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v (cancel is a clean no-change)", done)
	}
	if at := gitRun(t, dir, "rev-parse", "HEAD"); at != before {
		t.Errorf("HEAD moved on cancel: %s", at)
	}
}

// A target off the current branch parks the second, non-ancestor confirm
// after the mode pick.
func TestOpHTTPResetNonAncestorConfirm(t *testing.T) {
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-b", "side")
	localCommit(t, dir, "s.txt", "s\n", "side work")
	sideSha := gitRun(t, dir, "rev-parse", "side")
	gitRun(t, dir, "checkout", "main")
	before := gitRun(t, dir, "rev-parse", "HEAD")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpJSON(t, ts, `{"op":"reset","sha":"`+sideSha+`"}`)
	run := srv.opByID(opID)
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"soft"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "reset-confirm" {
		t.Fatalf("pending = %+v, want reset-confirm", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"cancel"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != false {
		t.Fatalf("done = %v", done)
	}
	if at := gitRun(t, dir, "rev-parse", "HEAD"); at != before {
		t.Errorf("HEAD moved on cancel: %s", at)
	}
}

func TestOpHTTPResetBadInput(t *testing.T) {
	dir := newRepoDir(t, 1)
	head := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"reset"}`,
		`{"op":"reset","sha":"main"}`,
		`{"op":"reset","sha":"` + head + `","mode":"yolo"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
