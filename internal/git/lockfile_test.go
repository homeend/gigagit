package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLockFilesFindsKnownLocks(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"index.lock", "packed-refs.lock", "notes.txt", "somedir.lock"} {
		if n == "somedir.lock" {
			if err := os.Mkdir(filepath.Join(dir, n), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := LockFiles(dir)
	if len(got) != 2 {
		t.Fatalf("want index.lock + packed-refs.lock, got %d: %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, l := range got {
		names[l.Name] = true
		if l.ModTime.IsZero() {
			t.Errorf("%s has no ModTime — the age hint drives the UI", l.Name)
		}
		if !filepath.IsAbs(l.Path) {
			t.Errorf("%s path %q is not absolute", l.Name, l.Path)
		}
	}
	if !names["index.lock"] || !names["packed-refs.lock"] {
		t.Fatalf("wrong locks reported: %v", names)
	}
}

func TestLockFilesEmptyWhenClean(t *testing.T) {
	if got := LockFiles(t.TempDir()); len(got) != 0 {
		t.Fatalf("clean dir should report nothing, got %+v", got)
	}
}

// In the main worktree the git dir and the common dir are the same directory;
// a lock there must be reported once, not twice.
func TestLockFilesDedupesRepeatedDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LockFiles(dir, dir, ""); len(got) != 1 {
		t.Fatalf("want 1 lock, got %d", len(got))
	}
}

func TestIsLockFilePath(t *testing.T) {
	for _, p := range []string{"/r/.git/index.lock", "/r/.git/HEAD.lock", "config.lock"} {
		if !IsLockFilePath(p) {
			t.Errorf("%s should be recognised", p)
		}
	}
	for _, p := range []string{"/r/.git/config", "/r/secrets.lock", "/r/.git/index", ""} {
		if IsLockFilePath(p) {
			t.Errorf("%s must NOT be treated as a removable git lock", p)
		}
	}
}

func TestIsLockError(t *testing.T) {
	// The real message, as git prints it and the runner wraps it.
	real := fmt.Errorf("git add failed (exit 128): %s", `fatal: Unable to create '/r/.git/index.lock': File exists.

Another git process seems to be running in this repository, e.g.
an editor opened by 'git commit'. Please make sure all processes
are terminated then try again.`)
	if !IsLockError(real) {
		t.Fatal("the canonical index.lock failure must be recognised")
	}
	// A ref lock prints the create line without the longer advisory.
	refLock := errors.New(`error: cannot lock ref 'refs/heads/main': Unable to create '/r/.git/refs/heads/main.lock': File exists`)
	if !IsLockError(refLock) {
		t.Fatal("a ref lock failure must be recognised")
	}
	for _, e := range []error{nil, errors.New("fatal: not a git repository"), errors.New("merge conflict in index.lock handling")} {
		if IsLockError(e) {
			t.Errorf("%v must not be classified as a lock error", e)
		}
	}
}
