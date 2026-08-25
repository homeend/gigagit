package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lockDeps is the standard buffered-events OpDeps for these tests: the ops
// under test emit a handful of Progress events and never block.
func lockDeps(repo GitOps) OpDeps {
	return OpDeps{Repo: repo, Events: make(chan Event, 64)}
}

// lockRepo builds a real repo and returns it plus its .git dir.
func lockRepo(t *testing.T) (GitOps, string) {
	t.Helper()
	dir, repo := newRepo(t)
	return repo, filepath.Join(dir, ".git")
}

func touchLock(t *testing.T, gitDir, name string) string {
	t.Helper()
	p := filepath.Join(gitDir, name)
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRemoveGitLocksRemovesIndexLock(t *testing.T) {
	t.Parallel()
	repo, gitDir := lockRepo(t)
	lock := touchLock(t, gitDir, "index.lock")

	res, err := RemoveGitLocks{Paths: []string{lock}}.Run(context.Background(), lockDeps(repo))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Changed {
		t.Fatal("removing a lock should report Changed")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("index.lock still present")
	}
	if !strings.Contains(res.Summary, "1 stale git lock") {
		t.Fatalf("summary = %q", res.Summary)
	}
}

func TestRemoveGitLocksMultiple(t *testing.T) {
	t.Parallel()
	repo, gitDir := lockRepo(t)
	a := touchLock(t, gitDir, "index.lock")
	b := touchLock(t, gitDir, "HEAD.lock")

	res, err := RemoveGitLocks{Paths: []string{a, b}}.Run(context.Background(), lockDeps(repo))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(res.Summary, "removed 2 stale git locks") {
		t.Fatalf("summary = %q", res.Summary)
	}
}

func TestRemoveGitLocksEmptyIsNoOp(t *testing.T) {
	t.Parallel()
	repo, _ := lockRepo(t)
	res, err := RemoveGitLocks{}.Run(context.Background(), lockDeps(repo))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Changed {
		t.Fatal("empty Paths must not report Changed")
	}
}

// A lock that vanished between the scan and the removal is the desired state,
// not an error — another process finished and cleaned up after itself.
func TestRemoveGitLocksAlreadyGone(t *testing.T) {
	t.Parallel()
	repo, gitDir := lockRepo(t)
	res, err := RemoveGitLocks{Paths: []string{filepath.Join(gitDir, "index.lock")}}.
		Run(context.Background(), lockDeps(repo))
	if err != nil {
		t.Fatalf("a lock that is already gone must not be an error: %v", err)
	}
	if res.Changed {
		t.Fatal("nothing was removed, so Changed must be false")
	}
}

// The guard is what stops a frontend bug from turning this op into an
// arbitrary file delete.
func TestRemoveGitLocksRefusesForeignPaths(t *testing.T) {
	t.Parallel()
	repo, gitDir := lockRepo(t)
	outside := filepath.Join(t.TempDir(), "index.lock")
	if err := os.WriteFile(outside, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, path, want string
	}{
		{"not a lockfile name", filepath.Join(gitDir, "config"), "not a git lockfile"},
		{"lock-suffixed but unknown", filepath.Join(gitDir, "secrets.lock"), "not a git lockfile"},
		{"outside the git dir", outside, "outside this repository"},
		{"relative path", "index.lock", "not an absolute path"},
		{"traversal", filepath.Join(gitDir, "..", "..", "index.lock"), "outside this repository"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RemoveGitLocks{Paths: []string{tc.path}}.Run(context.Background(), lockDeps(repo))
			if err == nil {
				t.Fatalf("%s should be refused", tc.path)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
	// A refused batch must delete nothing at all — the guard runs over every
	// path before the first removal.
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("a refused path was deleted anyway")
	}
}

// A batch is validated up front, so one bad path cancels the whole run rather
// than deleting the good ones first.
func TestRemoveGitLocksValidatesBeforeRemoving(t *testing.T) {
	t.Parallel()
	repo, gitDir := lockRepo(t)
	good := touchLock(t, gitDir, "index.lock")

	_, err := RemoveGitLocks{Paths: []string{good, filepath.Join(gitDir, "config")}}.
		Run(context.Background(), lockDeps(repo))
	if err == nil {
		t.Fatal("batch with an invalid path should fail")
	}
	if _, err := os.Stat(good); err != nil {
		t.Fatal("valid lock was removed even though the batch was refused")
	}
}
