package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPCreateTagLightweight(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"create-tag","tag":"v2"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "created lightweight tag v2") {
		t.Errorf("summary = %v", done["summary"])
	}
	if typ := gitRun(t, dir, "for-each-ref", "--format=%(objecttype)", "refs/tags/v2"); typ != "commit" {
		t.Errorf("objecttype = %q, want commit (lightweight)", typ)
	}
}

func TestOpHTTPCreateTagAnnotatedAtCommit(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	first := gitRun(t, dir, "rev-parse", "HEAD~1")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts,
		`{"op":"create-tag","tag":"v3","sha":"`+first+`","message":"the three"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if typ := gitRun(t, dir, "for-each-ref", "--format=%(objecttype)", "refs/tags/v3"); typ != "tag" {
		t.Errorf("objecttype = %q, want tag (annotated)", typ)
	}
	if target := gitRun(t, dir, "rev-parse", "v3^{commit}"); target != first {
		t.Errorf("target = %s, want %s", target, first)
	}
}

func TestOpHTTPCreateTagBadInput(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"create-tag"}`,
		`{"op":"create-tag","tag":"-v"}`,
		`{"op":"create-tag","tag":"v9","sha":"--all"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}

// Annotating force-recreates the tag as annotated AT ITS CURRENT TARGET,
// which the server reads itself — a wire-supplied sha must be ignored, or
// one POST could retarget any tag.
func TestOpHTTPAnnotateTag(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "tag", "v1", "HEAD~1") // lightweight, NOT at HEAD
	target := gitRun(t, dir, "rev-parse", "HEAD~1")
	head := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))

	// The body smuggles sha pointing at HEAD; the target must stay HEAD~1.
	events := readSSE(t, ts, startOpBody(t, ts,
		`{"op":"annotate-tag","tag":"v1","message":"now annotated","sha":"`+head+`"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if typ := gitRun(t, dir, "for-each-ref", "--format=%(objecttype)", "refs/tags/v1"); typ != "tag" {
		t.Errorf("objecttype = %q, want tag (annotated)", typ)
	}
	if got := gitRun(t, dir, "rev-parse", "v1^{commit}"); got != target {
		t.Errorf("target moved: %s, want %s (wire sha must be ignored)", got, target)
	}
	if msg := gitRun(t, dir, "tag", "-l", "--format=%(contents:subject)", "v1"); msg != "now annotated" {
		t.Errorf("message = %q", msg)
	}
}

func TestOpHTTPAnnotateTagRefusals(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "tag", "v1")
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", `{"op":"annotate-tag","tag":"nope","message":"m"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown tag: code = %d, want 404", code)
	}
	for _, body := range []string{
		`{"op":"annotate-tag","message":"m"}`,
		`{"op":"annotate-tag","tag":"-v1","message":"m"}`,
		`{"op":"annotate-tag","tag":"v1","message":"  "}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}

func TestOpHTTPPushTag(t *testing.T) {
	t.Parallel()
	_, clone := cloneWithOrigin(t)
	gitRun(t, clone, "tag", "v1")
	ts := serve(t, New(domain.Open(clone)))

	// One remote: resolved automatically, no decision parks.
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"push-tag","tag":"v1"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, clone, "ls-remote", "--tags", "origin", "v1"); !strings.Contains(out, "refs/tags/v1") {
		t.Errorf("tag not on origin: %q", out)
	}
}

func TestOpHTTPPushTagUnknown(t *testing.T) {
	t.Parallel()
	_, clone := cloneWithOrigin(t)
	ts := serve(t, New(domain.Open(clone)))

	if code := postJSON(t, ts, "/api/op", `{"op":"push-tag","tag":"ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"push-tag","tag":"-v"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}

// Deleting from the remote goes through the engine's own confirm — the
// decision parks in the browser modal; "delete" then pushes the deletion.
func TestOpHTTPDeleteRemoteTag(t *testing.T) {
	t.Parallel()
	_, clone := cloneWithOrigin(t)
	gitRun(t, clone, "tag", "v1")
	gitRun(t, clone, "push", "origin", "v1")
	srv := New(domain.Open(clone))
	ts := serve(t, srv)

	opID := startOpJSON(t, ts, `{"op":"delete-remote-tag","tag":"v1"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	run.mu.Lock()
	req := run.pending
	run.mu.Unlock()
	if req.ID != "delete-remote-tag" {
		t.Fatalf("pending = %+v, want the delete-remote-tag confirm", req)
	}
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, clone, "ls-remote", "--tags", "origin", "v1"); strings.TrimSpace(out) != "" {
		t.Errorf("tag still on origin: %q", out)
	}
	// The local tag survives — only the remote ref was deleted.
	if out := gitRun(t, clone, "tag", "--list", "v1"); strings.TrimSpace(out) == "" {
		t.Error("local tag deleted too")
	}
}

func TestOpHTTPDeleteRemoteTagUnknown(t *testing.T) {
	t.Parallel()
	_, clone := cloneWithOrigin(t)
	ts := serve(t, New(domain.Open(clone)))

	if code := postJSON(t, ts, "/api/op", `{"op":"delete-remote-tag","tag":"ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", code)
	}
}

func TestOpHTTPFastForward(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	tip := gitRun(t, dir, "rev-parse", "main")
	gitRun(t, dir, "branch", "behind", "HEAD~2")
	gitRun(t, dir, "checkout", "behind")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"fast-forward","branch":"main"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "behind"); got != tip {
		t.Errorf("behind = %s, want fast-forwarded to %s", got, tip)
	}
	if cur := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); cur != "behind" {
		t.Errorf("current branch = %q, want behind (ff moves the ref, not HEAD's branch)", cur)
	}
}

// The commit menu fast-forwards to the commit under the cursor, which has no
// branch name — so the op takes a sha as well as a branch.
func TestOpHTTPFastForwardToSha(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	tip := gitRun(t, dir, "rev-parse", "main")
	gitRun(t, dir, "branch", "behind", "HEAD~2")
	gitRun(t, dir, "checkout", "behind")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"fast-forward","sha":"`+tip+`"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "behind"); got != tip {
		t.Errorf("behind = %s, want fast-forwarded to %s", got, tip)
	}
}

// A sha that is not a descendant is the engine's refusal, not a crash — and a
// non-hex target is rejected before any git runs (the checkout lane's rule).
func TestOpHTTPFastForwardShaGuards(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	behind := gitRun(t, dir, "rev-parse", "HEAD~2")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"fast-forward","sha":"`+behind+`"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want a refusal", done)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"fast-forward","sha":"main"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("non-hex sha: code = %d, want 400", code)
	}
}

func TestOpHTTPFastForwardNotAhead(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	gitRun(t, dir, "branch", "behind", "HEAD~2")
	ts := serve(t, New(domain.Open(dir)))

	// From main, "behind" is an ancestor — the engine refuses politely.
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"fast-forward","branch":"behind"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want a refusal", done)
	}
	if err, _ := done["error"].(string); !strings.Contains(err, "not ahead") {
		t.Errorf("error = %q, want the not-ahead refusal", err)
	}
}

func TestOpHTTPFastForwardUnknownBranch(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", `{"op":"fast-forward","branch":"ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"fast-forward","branch":"-x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}
