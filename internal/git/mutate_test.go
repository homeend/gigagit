package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitAllAndCurrentBranch(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "second", true); err != nil {
		t.Fatalf("commit: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if c := st.Counts(); c.Staged+c.Unstaged != 0 {
		t.Fatalf("expected clean tree after commit -a, got %+v", c)
	}
}

func TestCreateBranchAndSwitch(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := repo.CreateBranch(context.Background(), "feature"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := repo.Switch(context.Background(), "feature"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	cur, err := repo.CurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature", cur)
	}
}

func TestCurrentBranchDetachedReturnsEmpty(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "second")
	gitIn(t, dir, "checkout", "HEAD~1") // detach
	cur, err := repo.CurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("detached HEAD should not error: %v", err)
	}
	if cur != "" {
		t.Fatalf("current branch = %q, want empty on detached HEAD", cur)
	}
}
