package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStashPushListPop(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.StashPush(context.Background(), "gg-test"); err != nil {
		t.Fatalf("stash push: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if c := st.Counts(); c.Unstaged != 0 {
		t.Fatalf("expected clean tree after stash, got %+v", c)
	}
	list, err := repo.StashList(context.Background())
	if err != nil {
		t.Fatalf("stash list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("stash list = %v, want 1 entry", list)
	}
	if err := repo.StashPop(context.Background()); err != nil {
		t.Fatalf("stash pop: %v", err)
	}
	st, _ = repo.Status(context.Background())
	if c := st.Counts(); c.Unstaged != 1 {
		t.Fatalf("expected change restored after pop, got %+v", c)
	}
}
