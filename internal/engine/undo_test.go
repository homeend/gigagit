package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUndoLastCommitMovesHeadBackAndStages(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	if err := repo.Commit(ctx, "second", true); err != nil {
		t.Fatalf("commit: %v", err)
	}

	res, err := UndoLastCommit{}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	st, _ := repo.Status(ctx)
	if st.Counts().Staged == 0 {
		t.Fatalf("expected staged changes after undo, got %+v", st.Counts())
	}
}

func TestUndoLastCommitRefusesWhenLastOpNotCommit(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	if err := repo.CreateBranch(ctx, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Switch(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	_ = dir

	_, err := UndoLastCommit{}.Run(ctx, OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("expected undo to refuse when the last operation was not a commit")
	}
}
