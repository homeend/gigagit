package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPullRebaseStrategy(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	root := filepath.Dir(clone)
	origin := filepath.Join(root, "origin.git")

	other := filepath.Join(root, "other")
	gitIn(t, root, "clone", origin, other)
	gitIn(t, other, "checkout", "main")
	os.WriteFile(filepath.Join(other, "remote.txt"), []byte("r\n"), 0o644)
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-m", "remote-commit")
	gitIn(t, other, "push", "origin", "main")

	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("l\n"), 0o644)
	gitIn(t, clone, "add", ".")
	gitIn(t, clone, "commit", "-m", "local-commit")

	if err := repo.Pull(context.Background(), "origin", "main", PullRebase); err != nil {
		t.Fatalf("rebase pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clone, "remote.txt")); err != nil {
		t.Fatalf("remote.txt missing after rebase: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clone, "local.txt")); err != nil {
		t.Fatalf("local.txt missing after rebase: %v", err)
	}
}

func TestIsDirty(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	dirty, err := repo.IsDirty(context.Background())
	if err != nil {
		t.Fatalf("is-dirty: %v", err)
	}
	if dirty {
		t.Fatal("fresh repo should be clean")
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	dirty, _ = repo.IsDirty(context.Background())
	if !dirty {
		t.Fatal("modified tracked file should be dirty")
	}
}

func TestRemoteForBranchDefaultsToOrigin(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}

	remote, err := repo.RemoteForBranch(context.Background(), "main")
	if err != nil {
		t.Fatalf("remote-for-branch: %v", err)
	}
	if remote != "origin" {
		t.Fatalf("remote = %q, want origin", remote)
	}
	_ = clone
}
