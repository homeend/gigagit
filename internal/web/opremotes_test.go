package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// remoteFeatureClone builds origin+clone where origin carries a "feat" branch
// the clone only knows as origin/feat (no local branch).
func remoteFeatureClone(t *testing.T) (origin, clone string) {
	t.Helper()
	origin, clone = cloneWithOrigin(t)
	gitRun(t, origin, "branch", "feat", "main")
	gitRun(t, clone, "fetch", "origin")
	return origin, clone
}

func TestOpHTTPCheckoutRemoteStay(t *testing.T) {
	t.Parallel()
	_, clone := remoteFeatureClone(t)
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout-remote","ref":"origin/feat","name":"feat"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if cur := gitRun(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); cur != "main" {
		t.Errorf("current = %q, want main (stay intent)", cur)
	}
	if at, want := gitRun(t, clone, "rev-parse", "feat"), gitRun(t, clone, "rev-parse", "origin/feat"); at != want {
		t.Errorf("feat = %s, want %s", at, want)
	}
	if up := gitRun(t, clone, "rev-parse", "--abbrev-ref", "feat@{upstream}"); up != "origin/feat" {
		t.Errorf("upstream = %q", up)
	}
}

func TestOpHTTPCheckoutRemoteSwitch(t *testing.T) {
	t.Parallel()
	_, clone := remoteFeatureClone(t)
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout-remote","ref":"origin/feat","name":"feat","switch":true}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if cur := gitRun(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); cur != "feat" {
		t.Errorf("current = %q, want feat (switch intent)", cur)
	}
}

// The ref is an identifier resolved against a fresh remote-branches read —
// an unknown one 404s before any op starts, and Remote/Branch never come
// from the wire.
func TestOpHTTPCheckoutRemoteRefusals(t *testing.T) {
	t.Parallel()
	_, clone := remoteFeatureClone(t)
	ts := serve(t, New(domain.Open(clone)))

	if code := postJSON(t, ts, "/api/op", `{"op":"checkout-remote","ref":"origin/ghost","name":"g"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown ref: code = %d, want 404", code)
	}
	for _, body := range []string{
		`{"op":"checkout-remote","name":"x"}`,
		`{"op":"checkout-remote","ref":"-o/f","name":"x"}`,
		`{"op":"checkout-remote","ref":"origin/feat"}`,
		`{"op":"checkout-remote","ref":"origin/feat","name":"-x"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}

func TestOpHTTPDeleteRemoteBranch(t *testing.T) {
	t.Parallel()
	origin, clone := remoteFeatureClone(t)
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startOpJSON(t, ts, `{"op":"delete-remote-branch","ref":"origin/feat"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "delete-remote-branch" {
		t.Fatalf("pending = %+v, want the delete-remote-branch confirm", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, origin, "branch", "--list", "feat"); strings.TrimSpace(out) != "" {
		t.Errorf("feat still on origin: %q", out)
	}
}

func TestOpHTTPDeleteRemoteBranchUnknown(t *testing.T) {
	t.Parallel()
	_, clone := remoteFeatureClone(t)
	ts := serve(t, New(domain.Open(clone)))

	if code := postJSON(t, ts, "/api/op", `{"op":"delete-remote-branch","ref":"origin/ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", code)
	}
}

// reset-remote is the one lane that presets Mode ("hard") and so skips every
// engine guard — the server enforces the TUI's rule that the target must be
// the remote counterpart of the CHECKED-OUT branch, and the client owns the
// confirm.
func TestOpHTTPResetRemote(t *testing.T) {
	t.Parallel()
	_, clone := remoteFeatureClone(t)
	localCommit(t, clone, "l.txt", "l\n", "local ahead work")
	want := gitRun(t, clone, "rev-parse", "origin/main")
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"reset-remote","ref":"origin/main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if at := gitRun(t, clone, "rev-parse", "HEAD"); at != want {
		t.Errorf("HEAD = %s, want the remote tip %s", at, want)
	}
	if cur := gitRun(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); cur != "main" {
		t.Errorf("current = %q", cur)
	}
}

func TestOpHTTPResetRemoteWrongBranch(t *testing.T) {
	t.Parallel()
	_, clone := remoteFeatureClone(t)
	ts := serve(t, New(domain.Open(clone)))

	// Checked out on main; origin/feat is another branch's counterpart.
	if code := postJSON(t, ts, "/api/op", `{"op":"reset-remote","ref":"origin/feat"}`, "application/json", "", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("code = %d, want 422", code)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"reset-remote","ref":"origin/ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", code)
	}
}

func TestOpHTTPPrune(t *testing.T) {
	t.Parallel()
	origin, clone := remoteFeatureClone(t)
	gitRun(t, origin, "branch", "-D", "feat") // deleted upstream, tracking ref stays
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"prune"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, clone, "branch", "-r", "--list", "origin/feat"); strings.TrimSpace(out) != "" {
		t.Errorf("tracking ref survived prune: %q", out)
	}
}
