package git

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ListFiles is the member-set probe behind domain's index and working-tree
// endpoints. The distinction it has to get right: `git ls-files` lists the
// INDEX, so a tracked file deleted from disk is STILL in the tracked list —
// only `--deleted` separates it out, and the working-tree lane subtracts it.
func TestListFilesTrackedAndDeleted(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // one commit: README.md
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Two more tracked files (one with a space, to prove -z beats git's quoting),
	// then one of them removed from disk but NOT from the index.
	const spaced = "a file.txt"
	for _, name := range []string{spaced, "dropped.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", spaced, "dropped.txt")
	gitRun(t, dir, "commit", "-m", "two more")

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// os.Remove, NOT `git rm`: `git rm` would drop the index entry too and
	// --deleted would come back empty.
	if err := os.Remove(filepath.Join(dir, "dropped.txt")); err != nil {
		t.Fatal(err)
	}

	tracked, err := repo.ListFiles(ctx, false)
	if err != nil {
		t.Fatalf("ListFiles(tracked): %v", err)
	}
	sort.Strings(tracked)
	want := []string{spaced, "README.md", "dropped.txt"}
	sort.Strings(want)
	if len(tracked) != len(want) {
		t.Fatalf("ListFiles(false) = %v, want %v", tracked, want)
	}
	for i := range want {
		if tracked[i] != want[i] {
			t.Fatalf("ListFiles(false) = %v, want %v (untracked.txt must be absent, dropped.txt present)", tracked, want)
		}
	}

	gone, err := repo.ListFiles(ctx, true)
	if err != nil {
		t.Fatalf("ListFiles(deleted): %v", err)
	}
	if len(gone) != 1 || gone[0] != "dropped.txt" {
		t.Fatalf("ListFiles(true) = %v, want exactly [dropped.txt]", gone)
	}
}
