package git

import (
	"context"
	"testing"
)

func TestWorktreeForBranchFindsMain(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt, err := repo.WorktreeForBranch(context.Background(), "main")
	if err != nil {
		t.Fatalf("worktree for branch: %v", err)
	}
	if wt == nil {
		t.Fatal("expected to find a worktree on main")
	}
	if wt.Branch != "main" {
		t.Fatalf("wt.Branch = %q, want main", wt.Branch)
	}
}

func TestWorktreeForBranchMissingReturnsNil(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt, err := repo.WorktreeForBranch(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wt != nil {
		t.Fatalf("expected nil, got %+v", wt)
	}
}

func TestCanFastForward(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	first, err := repo.CurrentBranch(context.Background())
	if err != nil || first == "" {
		t.Fatalf("current branch: %q err=%v", first, err)
	}
	gitIn(t, dir, "tag", "base")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "second")

	ff, err := repo.CanFastForward(context.Background(), "base", "HEAD")
	if err != nil {
		t.Fatalf("can-ff: %v", err)
	}
	if !ff {
		t.Fatal("base should be an ancestor of HEAD (fast-forwardable)")
	}
	ff, err = repo.CanFastForward(context.Background(), "HEAD", "base")
	if err != nil {
		t.Fatalf("can-ff reverse: %v", err)
	}
	if ff {
		t.Fatal("HEAD should NOT be an ancestor of base")
	}
}
