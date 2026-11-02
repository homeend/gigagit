package gitwatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitSource waits up to d for src on the watcher's channel.
func waitSource(t *testing.T, w *Watcher, src Source, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case got, ok := <-w.Events():
			if !ok {
				t.Fatalf("events channel closed before %v", src)
			}
			if got == src {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %v", src)
		}
	}
}

func TestWatcherFiresOnReflogWrite(t *testing.T) {
	dir := t.TempDir() // ext4 /tmp → inotify works
	logs := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(dir, dir, []Source{Reflog}), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Write logs/HEAD after the watch is established.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(logs, "HEAD"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitSource(t, w, Reflog, 2*time.Second)
}

func TestWatcherCloseClosesChannel(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(dir, dir, []Source{Worktrees}), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-w.Events(); ok {
		t.Error("events channel should be closed after Close")
	}
}

func TestWatcherFiresOnNestedRefCreate(t *testing.T) {
	gitdir := t.TempDir()
	heads := filepath.Join(gitdir, "refs", "heads", "feature")
	if err := os.MkdirAll(heads, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(gitdir, gitdir, []Source{Branches}), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	time.Sleep(20 * time.Millisecond)
	// write refs/heads/feature/foo (a nested loose ref)
	if err := os.WriteFile(filepath.Join(heads, "foo"), []byte("deadbeef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitSource(t, w, Branches, 2*time.Second)
}

func TestWatcherFiresOnNewNamespaceDir(t *testing.T) {
	gitdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitdir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(gitdir, gitdir, []Source{Branches}), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	time.Sleep(20 * time.Millisecond)
	// create a new namespace dir, then a ref inside it
	ns := filepath.Join(gitdir, "refs", "heads", "team")
	if err := os.MkdirAll(ns, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let the new-dir Add land
	if err := os.WriteFile(filepath.Join(ns, "x"), []byte("c0ffee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitSource(t, w, Branches, 2*time.Second)
}

func TestWatcherSeesBranchesInNewNestedDirs(t *testing.T) {
	t.Parallel()
	c := t.TempDir()
	heads := filepath.Join(c, "refs", "heads")
	if err := os.MkdirAll(heads, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(c, c, []Source{Branches}), 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.MkdirAll(filepath.Join(heads, "x", "y"), 0o755); err != nil {
		t.Fatal(err)
	}
	drainFor(w, 200*time.Millisecond)
	if err := os.WriteFile(filepath.Join(heads, "x", "y", "two"), []byte("sha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !sawSource(w, Branches, 2*time.Second) {
		t.Fatal("a branch two levels down went unseen")
	}
}

func TestWatcherSeesTheFirstWorktree(t *testing.T) {
	t.Parallel()
	c := t.TempDir()
	if err := os.MkdirAll(filepath.Join(c, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(c, c, []Source{Branches, Worktrees}), 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.MkdirAll(filepath.Join(c, "worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !sawSource(w, Worktrees, 2*time.Second) {
		t.Fatal("worktrees/ appearing went unseen")
	}
	drainFor(w, 200*time.Millisecond)
	if err := os.MkdirAll(filepath.Join(c, "worktrees", "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !sawSource(w, Worktrees, 2*time.Second) {
		t.Fatal("the first worktree went unseen")
	}
}

func TestWatcherSeesALinkedWorktreeSwitch(t *testing.T) {
	t.Parallel()
	c := t.TempDir()
	wt := filepath.Join(c, "worktrees", "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := New(Plan(c, c, []Source{Worktrees}), 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, base := range []string{"HEAD", "gitdir"} {
		drainFor(w, 100*time.Millisecond)
		if err := os.WriteFile(filepath.Join(wt, base), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !sawSource(w, Worktrees, 2*time.Second) {
			t.Fatalf("worktrees/wt/%s went unseen", base)
		}
	}
	drainFor(w, 100*time.Millisecond)
	if err := os.WriteFile(filepath.Join(wt, "index"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if sawSource(w, Worktrees, 200*time.Millisecond) {
		t.Fatal("index churn must stay quiet")
	}
}

func drainFor(w *Watcher, d time.Duration) {
	end := time.After(d)
	for {
		select {
		case <-w.Events():
		case <-end:
			return
		}
	}
}

func sawSource(w *Watcher, s Source, d time.Duration) bool {
	end := time.After(d)
	for {
		select {
		case got := <-w.Events():
			if got == s {
				return true
			}
		case <-end:
			return false
		}
	}
}
