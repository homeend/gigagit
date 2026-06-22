package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReflogEntriesListsHeadActions(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// Two more HEAD-moving actions on top of the initial commit.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "second commit")
	gitIn(t, dir, "checkout", "-b", "feature")

	entries, err := repo.ReflogEntries(context.Background(), 50)
	if err != nil {
		t.Fatalf("reflog entries: %v", err)
	}
	if len(entries) < 3 {
		t.Fatalf("want >=3 entries, got %d: %+v", len(entries), entries)
	}
	top := entries[0]
	if top.Selector != "HEAD@{0}" {
		t.Fatalf("top selector = %q, want HEAD@{0}", top.Selector)
	}
	if len(top.Hash) != 40 {
		t.Fatalf("top hash = %q, want a full 40-char SHA", top.Hash)
	}
	if top.ShortHash == "" || top.Subject == "" {
		t.Fatalf("top entry missing short hash or subject: %+v", top)
	}
	// The most recent action was the checkout.
	if !strings.Contains(top.Subject, "checkout") {
		t.Fatalf("top subject = %q, want it to mention checkout", top.Subject)
	}
}

func TestReflogEntriesRespectsLimit(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	for i := 0; i < 4; i++ {
		gitIn(t, dir, "commit", "--allow-empty", "-m", "c")
	}
	entries, err := repo.ReflogEntries(context.Background(), 2)
	if err != nil {
		t.Fatalf("reflog entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want exactly 2 entries under limit, got %d", len(entries))
	}
}

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
