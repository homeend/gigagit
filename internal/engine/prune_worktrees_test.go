package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPruneWorktreesRemovesStaleAdminDir(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitE(t, dir, "worktree", "add", wt, "-b", "tmp-branch")
	// Simulate a worktree whose directory vanished (the stale state prune cleans).
	os.RemoveAll(wt)

	res, err := PruneWorktrees{}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	admin := filepath.Join(dir, ".git", "worktrees")
	entries, _ := os.ReadDir(admin)
	if len(entries) != 0 {
		t.Fatalf("stale admin dirs remain: %v", entries)
	}
}
