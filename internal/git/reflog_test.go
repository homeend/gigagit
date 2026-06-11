package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastReflogSubjectIsCommit(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	subj, err := repo.LastReflogSubject(context.Background())
	if err != nil {
		t.Fatalf("last reflog subject: %v", err)
	}
	if !strings.HasPrefix(subj, "commit") {
		t.Fatalf("subject = %q, want it to start with 'commit'", subj)
	}
}

func TestResetSoftMovesRefKeepsWorkingTree(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "second")

	before := revParse(t, dir, "HEAD")
	if err := repo.ResetSoft(context.Background(), "HEAD@{1}"); err != nil {
		t.Fatalf("reset soft: %v", err)
	}
	after := revParse(t, dir, "HEAD")
	if before == after {
		t.Fatal("HEAD did not move after reset --soft HEAD@{1}")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatalf("b.txt lost after soft reset: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if st.Counts().Staged == 0 {
		t.Fatalf("expected the undone commit's changes to be staged, got %+v", st.Counts())
	}
}
