package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStageBlobSetsIndexNotWorktree(t *testing.T) {
	dir, runner := newTestRepo(t) // README.md = "hello\n" committed & in index
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// Modify the working tree so index != working tree.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("WORKING\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Stage a DIFFERENT content than either side (proves we set the index to
	// exactly the bytes given, and the working tree is untouched).
	if err := repo.StageBlob(ctx, "README.md", []byte("STAGED\n")); err != nil {
		t.Fatal(err)
	}

	// Working tree unchanged on disk.
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "WORKING\n" {
		t.Fatalf("working tree = %q, want WORKING (untouched)", b)
	}
	// Index now holds STAGED.
	out, err := exec.Command("git", "-C", dir, "show", ":README.md").CombinedOutput()
	if err != nil {
		t.Fatalf("git show :README.md: %v\n%s", err, out)
	}
	if string(out) != "STAGED\n" {
		t.Fatalf("index = %q, want STAGED", out)
	}
}
