package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTopLevelReturnsRepoRoot(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	got, err := repo.TopLevel(context.Background())
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	// git may resolve symlinks (e.g. /var -> /private/var on macOS); compare by
	// resolving both sides.
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Fatalf("TopLevel = %q, want %q", gotResolved, wantResolved)
	}
}

func TestCheckRefFormatBranch(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := repo.CheckRefFormatBranch(context.Background(), "feature/ok-1"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
	if err := repo.CheckRefFormatBranch(context.Background(), "bad..name"); err == nil {
		t.Error("invalid name 'bad..name' should be rejected")
	}
}

func TestAddWorktreeCreatesDirAndBranch(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wtPath := filepath.Join(filepath.Dir(dir), "wt-feature")
	err := repo.AddWorktree(context.Background(), wtPath, "feature/x", "main", nil)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	// The worktree directory exists and contains the committed file.
	if _, statErr := os.Stat(filepath.Join(wtPath, "README.md")); statErr != nil {
		t.Fatalf("worktree checkout missing: %v", statErr)
	}
	// The new branch exists.
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/feature/x")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("new branch not created: %v\n%s", e, out)
	}
}
