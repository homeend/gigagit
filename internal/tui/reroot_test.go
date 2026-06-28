package tui

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// runGit runs a git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestReRootPointsAtNewWorktreeAndReloads(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := New(domain.New(repo))
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)

	// Create a sibling worktree on a new branch.
	wt := filepath.Join(filepath.Dir(dir), "wt-reroot")
	runGit(t, dir, "worktree", "add", "-b", "feature/r", wt, "main")

	updated, cmd := m.reRoot(wt)
	m = updated.(Model)
	if m.switchTarget != wt {
		t.Fatalf("switchTarget = %q, want %q", m.switchTarget, wt)
	}
	if !m.loading {
		t.Error("reRoot should put the model into the loading state")
	}
	if cmd == nil {
		t.Fatal("reRoot should return a reload command")
	}
	// Apply the reload; the model should now be rooted in the new worktree.
	// reRoot now returns a batch (loadCmd + startWatchCmd); run each sub-cmd and
	// update the model so both dataLoadedMsg and watchReadyMsg are processed.
	for _, subcmd := range batchCmds(cmd) {
		updated2, _ := m.Update(subcmd())
		m = updated2.(Model)
	}
	// Close any watcher opened during the test to avoid goroutine leaks.
	t.Cleanup(func() {
		if m.watcher != nil {
			_ = m.watcher.Close()
		}
	})
	resolvedWant, _ := filepath.EvalSymlinks(wt)
	resolvedGot, _ := filepath.EvalSymlinks(m.currentWorktree)
	if resolvedGot != resolvedWant {
		t.Fatalf("after reRoot currentWorktree = %q, want %q", resolvedGot, resolvedWant)
	}
	if m.status.Branch != "feature/r" {
		t.Fatalf("after reRoot branch = %q, want feature/r", m.status.Branch)
	}
}
