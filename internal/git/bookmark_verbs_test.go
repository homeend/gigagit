package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlobSHAAndCatFile(t *testing.T) {
	dir, runner := newTestRepo(t) // README.md committed as "hello\n"
	r := &Repo{Runner: runner}
	ctx := context.Background()
	sha, err := r.BlobSHA(ctx, "HEAD", "README.md")
	if err != nil || sha == "" {
		t.Fatalf("BlobSHA: %q err %v", sha, err)
	}
	data, err := r.CatFileBlob(ctx, sha)
	if err != nil {
		t.Fatalf("CatFileBlob: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("blob = %q, want hello", data)
	}
	_ = dir
}

func TestShowFileInDirReadsLinkedWorktreeIndex(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, dir, "worktree", "add", "-b", "wtbr", wt)
	// Stage differing content in the linked worktree, then make the working
	// file newer so staged ≠ working.
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", "README.md")
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("working\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, err := r.ShowFileInDir(ctx, wt, "", "README.md") // git -C wt show :README.md
	if err != nil {
		t.Fatalf("ShowFileInDir: %v", err)
	}
	if strings.TrimRight(string(staged), "\n") != "staged" {
		t.Fatalf("staged side = %q, want staged", staged)
	}
}
