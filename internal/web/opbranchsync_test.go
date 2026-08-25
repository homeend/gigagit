package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// featureBranchClone extends cloneWithOrigin with a second branch that exists
// on both sides and is NOT checked out — the shape both branch-scoped ops
// need, since the whole point is acting on a branch you are not standing on.
func featureBranchClone(t *testing.T) (origin, clone string) {
	t.Helper()
	origin, clone = cloneWithOrigin(t)
	gitRun(t, clone, "checkout", "-b", "feature")
	gitRun(t, clone, "push", "-u", "origin", "feature")
	gitRun(t, clone, "checkout", "main")
	return origin, clone
}

// pushRemoteCommitOn is pushRemoteCommit for a branch other than main.
func pushRemoteCommitOn(t *testing.T, origin, branch, file, content, msg string) {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "w")
	gitRun(t, dir, "clone", "-b", branch, origin, work)
	if err := os.WriteFile(filepath.Join(work, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "add", "-A")
	gitRun(t, work, "commit", "-m", msg)
	gitRun(t, work, "push", "origin", branch)
}

// Pulling a NAMED branch must advance that branch and leave HEAD alone — the
// axis that inverts silently if the op is dispatched with the wrong intent
// (PullAndStay would check the branch out).
func TestOpHTTPPullNamedBranchStaysHere(t *testing.T) {
	t.Parallel()
	origin, clone := featureBranchClone(t)
	pushRemoteCommitOn(t, origin, "feature", "r.txt", "r\n", "remote feature work")

	mainBefore := gitRun(t, clone, "rev-parse", "main")
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"pull","branch":"feature"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}

	if got, want := gitRun(t, clone, "rev-parse", "feature"), gitRun(t, clone, "rev-parse", "origin/feature"); got != want {
		t.Errorf("feature = %s, origin/feature = %s — the branch did not advance", got, want)
	}
	if head := gitRun(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); head != "main" {
		t.Errorf("HEAD = %s, want main — a background pull must not switch branches", head)
	}
	if got := gitRun(t, clone, "rev-parse", "main"); got != mainBefore {
		t.Errorf("main moved to %s, want %s", got, mainBefore)
	}
}

// No branch in the request keeps the old meaning: pull the current one.
func TestOpHTTPPullNoBranchPullsCurrent(t *testing.T) {
	t.Parallel()
	origin, clone := cloneWithOrigin(t)
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"pull"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got, want := gitRun(t, clone, "rev-parse", "main"), gitRun(t, clone, "rev-parse", "origin/main"); got != want {
		t.Errorf("main = %s, origin/main = %s", got, want)
	}
}

// Pushing a NAMED branch must publish that branch, not whatever is checked
// out — the mirror of the pull axis above.
func TestOpHTTPPushNamedBranch(t *testing.T) {
	t.Parallel()
	origin, clone := featureBranchClone(t)
	// A local-only commit on feature, made without leaving main.
	gitRun(t, clone, "checkout", "feature")
	if err := os.WriteFile(filepath.Join(clone, "l.txt"), []byte("l\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "add", "-A")
	gitRun(t, clone, "commit", "-m", "local feature work")
	gitRun(t, clone, "checkout", "main")

	want := gitRun(t, clone, "rev-parse", "feature")
	mainBefore := gitRun(t, clone, "rev-parse", "main")
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"push","branch":"feature"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}

	if got := gitRun(t, origin, "rev-parse", "refs/heads/feature"); got != want {
		t.Errorf("origin feature = %s, want %s", got, want)
	}
	// main was never pushed: the op must not have fallen back to the current branch.
	if got := gitRun(t, origin, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Errorf("origin main = %s, want %s — push used the wrong branch", got, mainBefore)
	}
	if head := gitRun(t, clone, "rev-parse", "--abbrev-ref", "HEAD"); head != "main" {
		t.Errorf("HEAD = %s, want main", head)
	}
}

// A name git would read as an OPTION is refused before it reaches argv, on
// both branch-scoped ops. --upload-pack / --receive-pack are the ones that
// matter here: git executes their value on the far side of a fetch/push.
func TestOpHTTPBranchSyncRejectsOptionLikeNames(t *testing.T) {
	t.Parallel()
	_, clone := featureBranchClone(t)
	ts := serve(t, New(domain.Open(clone)))

	for _, body := range []string{
		`{"op":"pull","branch":"--upload-pack=touch /tmp/x"}`,
		`{"op":"push","branch":"--receive-pack=touch /tmp/x"}`,
		`{"op":"pull","branch":"-x"}`,
		`{"op":"push","branch":"-x"}`,
	} {
		var out map[string]any
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", &out); code != 400 {
			t.Errorf("%s: code = %d, want 400", body, code)
		}
	}
}

// The converse, and the reason the guard is a leading-dash check rather than
// a character allowlist: gg builds argv and never runs git through a shell,
// so shell metacharacters are ordinary bytes in a branch name. Tightening
// isGitArgSafe to reject them would refuse legal branches while buying
// nothing — this test fails if someone "hardens" it that way.
func TestOpHTTPBranchSyncAcceptsOddButLegalNames(t *testing.T) {
	t.Parallel()
	origin, clone := featureBranchClone(t)
	gitRun(t, clone, "branch", "odd;name$x")
	gitRun(t, clone, "push", "-u", "origin", "odd;name$x")
	pushRemoteCommitOn(t, origin, "odd;name$x", "o.txt", "o\n", "remote odd work")
	ts := serve(t, New(domain.Open(clone)))

	var out struct {
		OpID string `json:"op_id"`
	}
	code := postJSON(t, ts, "/api/op", `{"op":"pull","branch":"odd;name$x"}`, "application/json", "", &out)
	if code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202 — a legal branch name was refused", code)
	}
	events := readSSE(t, ts, out.OpID, 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got, want := gitRun(t, clone, "rev-parse", "odd;name$x"), gitRun(t, clone, "rev-parse", "origin/odd;name$x"); got != want {
		t.Errorf("odd;name$x = %s, origin copy = %s", got, want)
	}
}
