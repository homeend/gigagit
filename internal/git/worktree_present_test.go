package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// WorktreeFilesPresent stats, and that is the point: git has no listing that
// answers "is this on disk". A SKIP-WORKTREE entry removed from disk is
// invisible to `ls-files --deleted`, so on a sparse checkout the git-based
// probe would call every sparse-excluded path present.
func TestWorktreeFilesPresent(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // one commit: README.md
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	for _, name := range []string{"sparse.txt", "dropped.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", "sparse.txt", "dropped.txt")
	gitRun(t, dir, "commit", "-m", "two tracked files")

	gitRun(t, dir, "update-index", "--skip-worktree", "sparse.txt")
	for _, name := range []string{"sparse.txt", "dropped.txt"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pin the git behaviour this verb exists to route around: --deleted sees
	// the plain removal and NOT the skip-worktree one.
	res, err := runner.Run(ctx, "git ls-files (deleted probe)", []string{"ls-files", "-z", "--deleted"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Stdout; got != "dropped.txt\x00" {
		t.Logf("ls-files --deleted = %q (this test's premise is that sparse.txt is absent from it)", got)
		if strings.Contains(got, "sparse.txt") {
			t.Fatalf("this git reports a skip-worktree removal in --deleted (%q) — the premise changed, revisit WorktreeFilesPresent", got)
		}
	}

	got, err := repo.WorktreeFilesPresent(ctx, []string{
		"README.md", "sparse.txt", "dropped.txt", "untracked.txt", "never.txt", "../escape.txt",
	})
	if err != nil {
		t.Fatalf("WorktreeFilesPresent: %v", err)
	}
	want := map[string]bool{
		"README.md":     true,  // tracked and on disk
		"sparse.txt":    false, // skip-worktree + removed: git cannot see this
		"dropped.txt":   false, // plainly removed
		"untracked.txt": true,  // on disk is on disk, tracked or not
		"never.txt":     false,
		"../escape.txt": false, // escapes the tree, so it is not a member of it
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries for %d paths: %v", len(got), len(want), got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("present[%q] = %v, want %v", k, got[k], w)
		}
	}
}
