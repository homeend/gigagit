package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// With --untracked-files=all, an entirely-untracked directory must be reported
// as its individual files, never collapsed into a single "dir/" entry.
func TestStatusListsUntrackedFilesNotDirectories(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := repo.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	got := map[string]bool{}
	for _, f := range st.Files {
		got[f.Path] = true
		if f.Path == "sub" || f.Path == "sub/" {
			t.Fatalf("status collapsed an untracked dir into %q; want individual files: %v", f.Path, st.Files)
		}
	}
	for _, want := range []string{"sub/a.txt", "sub/b.txt"} {
		if !got[want] {
			t.Fatalf("expected %q among untracked files, got %v", want, st.Files)
		}
	}
	if c := st.Counts().Untracked; c != 2 {
		t.Fatalf("untracked count = %d, want 2", c)
	}
}
