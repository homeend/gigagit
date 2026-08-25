package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPContinueMerge(t *testing.T) {
	t.Parallel()
	dir := conflictingRepo(t)
	conflictedMergeState(t, dir)
	// resolve by hand, stage — exactly the state Continue is for
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "f.txt")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"continue"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("unmerged entries survived continue:\n%s", out)
	}
	if log := gitRun(t, dir, "log", "--merges", "--oneline"); log == "" {
		t.Error("no merge commit after continue")
	}
}

func TestOpHTTPAbortMerge(t *testing.T) {
	t.Parallel()
	dir := conflictingRepo(t)
	pre := gitRun(t, dir, "rev-parse", "HEAD")
	conflictedMergeState(t, dir)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"abort"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "ls-files", "-u"); out != "" {
		t.Errorf("unmerged entries survived abort:\n%s", out)
	}
	if head := gitRun(t, dir, "rev-parse", "HEAD"); head != pre {
		t.Errorf("HEAD = %s, want pre-merge %s", head, pre)
	}
	if got := strings.TrimSpace(gitRun(t, dir, "show", "HEAD:f.txt")); got != "main" {
		t.Errorf("f.txt @HEAD = %q", got)
	}
}

// Nothing paused: the engine refuses; the wire reports ok=false, repo untouched.
func TestOpHTTPContinueNothingPaused(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"continue"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
}
