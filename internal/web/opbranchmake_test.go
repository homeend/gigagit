package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// jsonString quotes a value for splicing into a hand-written request body —
// a t.TempDir path can contain characters that need escaping.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// These reuse oprun_test.go's twoBranchRepo: main checked out, plus a "side"
// branch holding a commit main lacks — enough to tell "created from HERE"
// apart from "created from HEAD".

func TestOpHTTPCreateBranchFromStartPoint(t *testing.T) {
	t.Parallel()
	dir := twoBranchRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"create-branch","name":"topic","branch":"side"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	// Created AT side's tip, not at HEAD (main) — the axis a swapped
	// field would invert silently.
	if got, want := gitRun(t, dir, "rev-parse", "topic"), gitRun(t, dir, "rev-parse", "side"); got != want {
		t.Errorf("topic = %s, side = %s — wrong start point", got, want)
	}
	// Creating a branch must not check it out.
	if head := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); head != "main" {
		t.Errorf("HEAD = %s, want main", head)
	}
}

// No start point means HEAD, the git default.
func TestOpHTTPCreateBranchDefaultsToHead(t *testing.T) {
	t.Parallel()
	dir := twoBranchRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"create-branch","name":"from-head"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got, want := gitRun(t, dir, "rev-parse", "from-head"), gitRun(t, dir, "rev-parse", "main"); got != want {
		t.Errorf("from-head = %s, main = %s", got, want)
	}
}

// An illegal ref name gets past isGitArgSafe (no leading dash) and is refused
// by the engine's check-ref-format, which is where the clear message lives.
func TestOpHTTPCreateBranchIllegalNameFailsInOp(t *testing.T) {
	t.Parallel()
	dir := twoBranchRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"create-branch","name":"bad..name"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want a failure", done)
	}
	if n := gitRun(t, dir, "branch", "--list", "bad..name"); strings.TrimSpace(n) != "" {
		t.Errorf("a branch was created anyway: %q", n)
	}
}

func TestOpHTTPRenameBranch(t *testing.T) {
	t.Parallel()
	dir := twoBranchRepo(t)
	want := gitRun(t, dir, "rev-parse", "side")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"rename-branch","branch":"side","name":"renamed"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "rev-parse", "renamed"); got != want {
		t.Errorf("renamed = %s, want %s", got, want)
	}
	if out := gitRun(t, dir, "branch", "--list", "side"); strings.TrimSpace(out) != "" {
		t.Errorf("old name still present: %q", out)
	}
}

func TestOpHTTPCreateWorktreeForBranch(t *testing.T) {
	t.Parallel()
	dir := twoBranchRepo(t)
	// A sibling of the repo, so the new worktree is not nested inside it.
	wtPath := filepath.Join(filepath.Dir(dir), "wt-"+filepath.Base(dir))
	t.Cleanup(func() { os.RemoveAll(wtPath) })
	ts := serve(t, New(domain.Open(dir)))

	body := `{"op":"create-worktree","branch":"side","path":` + jsonString(wtPath) + `}`
	events := readSSE(t, ts, startOpJSON(t, ts, body), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}

	if _, err := os.Stat(filepath.Join(wtPath, ".git")); err != nil {
		t.Fatalf("no worktree at %s: %v", wtPath, err)
	}
	if head := gitRun(t, wtPath, "rev-parse", "--abbrev-ref", "HEAD"); head != "side" {
		t.Errorf("new worktree HEAD = %s, want side", head)
	}
	// The serving worktree must not have moved.
	if head := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); head != "main" {
		t.Errorf("original HEAD = %s, want main", head)
	}
}

// A branch already checked out somewhere cannot get a second worktree; the
// refusal must arrive as a failed op, not a hang or a partial worktree.
func TestOpHTTPCreateWorktreeRefusesCheckedOutBranch(t *testing.T) {
	t.Parallel()
	dir := twoBranchRepo(t)
	wtPath := filepath.Join(filepath.Dir(dir), "wt2-"+filepath.Base(dir))
	t.Cleanup(func() { os.RemoveAll(wtPath) })
	ts := serve(t, New(domain.Open(dir)))

	body := `{"op":"create-worktree","branch":"main","path":` + jsonString(wtPath) + `}`
	events := readSSE(t, ts, startOpJSON(t, ts, body), 60*time.Second)
	if done := events[len(events)-1]; done["ok"] != false {
		t.Fatalf("done = %v, want a refusal (main is checked out here)", done)
	}
	if _, err := os.Stat(wtPath); err == nil {
		t.Errorf("a worktree was created at %s despite the refusal", wtPath)
	}
}

func TestOpHTTPBranchMakeRejectsBadInput(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(twoBranchRepo(t))))

	for _, body := range []string{
		`{"op":"create-branch"}`,                               // no name
		`{"op":"create-branch","name":"-x"}`,                   // option-like name
		`{"op":"create-branch","name":"ok","branch":"-x"}`,     // option-like start point
		`{"op":"rename-branch","branch":"side"}`,               // no new name
		`{"op":"rename-branch","name":"x"}`,                    // no old name
		`{"op":"rename-branch","branch":"side","name":"-x"}`,   // option-like new name
		`{"op":"create-worktree","branch":"side"}`,             // no path
		`{"op":"create-worktree","branch":"side","path":"-x"}`, // option-like path
		`{"op":"create-worktree","path":"/tmp/x"}`,             // no branch
	} {
		var out map[string]any
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", &out); code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", body, code)
		}
	}
}
