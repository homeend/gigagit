package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// gitCmd builds a git command like gitRun but returns it unrun — for
// invocations whose non-zero exit is the EXPECTED outcome (a conflicting
// stash apply / merge).
func gitCmd(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	return cmd
}

// conflictedStashApply builds the standalone-conflict state: a stash whose
// apply conflicts with a later commit, plus an unrelated untracked file the
// discard must preserve.
func conflictedStashApply(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("stashed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "stash")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "c2")
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// stash apply exits 1 on conflict — expected, so not gitRun
	cmd := gitCmd(t, dir, "stash", "apply")
	if err := cmd.Run(); err == nil {
		t.Fatal("stash apply should conflict")
	}
	if out := gitRun(t, dir, "status", "--porcelain"); !strings.Contains(out, "UU f.txt") {
		t.Fatalf("no conflict fabricated: %q", out)
	}
	return dir
}

type statusConflictWire struct {
	Conflict *struct {
		Op         string `json:"op"`
		Desc       string `json:"desc"`
		Conflicted int    `json:"conflicted"`
		Standalone bool   `json:"standalone"`
	} `json:"conflict"`
}

// TestStatusStandaloneConflict: unmerged paths with no paused sequencer op
// surface as a standalone conflict payload — previously absent entirely,
// which left the client with no bar and no way out.
func TestStatusStandaloneConflict(t *testing.T) {
	dir := conflictedStashApply(t)
	ts := serve(t, New(domain.Open(dir)))

	var got statusConflictWire
	if code := getJSON(t, ts, "/api/status", &got); code != http.StatusOK {
		t.Fatalf("status code = %d", code)
	}
	if got.Conflict == nil || !got.Conflict.Standalone || got.Conflict.Op != "apply" || got.Conflict.Conflicted != 1 {
		t.Fatalf("conflict = %+v", got.Conflict)
	}
}

// TestOpAbortApply: the discard clears the conflict, keeps the unrelated
// untracked file AND the stash entry (retryable), and moves f.txt back to
// HEAD's content.
func TestOpAbortApply(t *testing.T) {
	dir := conflictedStashApply(t)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"abort-apply"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "status", "--porcelain"); strings.Contains(out, "UU") || !strings.Contains(out, "?? other.txt") {
		t.Fatalf("post-discard status = %q", out)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(b) != "committed\n" {
		t.Errorf("f.txt = %q, want HEAD content", b)
	}
	if out := gitRun(t, dir, "stash", "list"); !strings.Contains(out, "stash@{0}") {
		t.Errorf("stash gone after discard: %q", out)
	}
}

// TestOpAbortApplyRefusals: a clean tree and a PAUSED-op conflict both
// refuse — the paused case belongs to abort's sequencer cleanup.
func TestOpAbortApplyRefusals(t *testing.T) {
	// clean tree
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/op", `{"op":"abort-apply"}`, "application/json", "", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("clean-tree code = %d, want 422", code)
	}

	// paused merge conflict
	dir2 := newRepoDir(t, 1)
	gitRun(t, dir2, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir2, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir2, "commit", "-am", "side")
	gitRun(t, dir2, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir2, "f.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir2, "commit", "-am", "main2")
	if err := gitCmd(t, dir2, "merge", "side").Run(); err == nil {
		t.Fatal("merge should conflict")
	}
	ts2 := serve(t, New(domain.Open(dir2)))
	var got map[string]string
	if code := postJSON(t, ts2, "/api/op", `{"op":"abort-apply"}`, "application/json", "", &got); code != http.StatusUnprocessableEntity {
		t.Errorf("paused-merge code = %d, want 422", code)
	}
	// the paused-op payload still reports the merge, NOT standalone
	var st statusConflictWire
	if code := getJSON(t, ts2, "/api/status", &st); code != http.StatusOK {
		t.Fatal("status failed")
	}
	if st.Conflict == nil || st.Conflict.Standalone || st.Conflict.Op != "merge" {
		t.Errorf("paused conflict payload = %+v", st.Conflict)
	}
}
