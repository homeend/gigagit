package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// A decided fork must leave a "resolved" marker in the replay history:
// a reconnecting client re-shows then re-hides the modal (idempotent
// replay), and a second tab's modal closes live off the same event.
func TestDecidePublishesResolved(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "branch", "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second) // fresh subscribe: full replay
	di, ri := -1, -1
	for i, we := range events {
		if we["type"] == "decision" && di == -1 {
			di = i
		}
		if we["type"] == "resolved" && ri == -1 {
			ri = i
		}
	}
	if di == -1 || ri == -1 || ri < di {
		t.Fatalf("replay must contain decision then resolved, got %v", events)
	}
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
}

// The stash ops' optional sha is a freshness guard on top of the ref
// allowlist: a mismatch means the stash list changed under the client.
func TestOpHTTPStashShaGuard(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "keepme")
	ts := serve(t, New(domain.Open(dir)))

	code := postJSON(t, ts, "/api/op", `{"op":"stash-drop","ref":"stash@{0}","sha":"`+strings.Repeat("0", 40)+`"}`, "application/json", "", nil)
	if code != http.StatusConflict {
		t.Fatalf("mismatched sha code = %d, want 409", code)
	}
	if out := gitRun(t, dir, "stash", "list"); !strings.Contains(out, "keepme") {
		t.Fatalf("stash gone after refused drop: %q", out)
	}

	// matching sha dispatches; the empty-sha path is covered by the
	// existing apply/pop/drop tests
	sha := gitRun(t, dir, "rev-parse", "stash@{0}")
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-apply","ref":"stash@{0}","sha":"`+sha+`"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
}
