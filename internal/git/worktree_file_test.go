package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteWorktreeFile(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // has README.md committed
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	got, err := repo.ReadWorktreeFile(ctx, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("read = %q, want hello", got)
	}

	if err := repo.WriteWorktreeFile(ctx, "README.md", []byte("changed\n")); err != nil {
		t.Fatal(err)
	}
	on, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(on) != "changed\n" {
		t.Fatalf("on-disk = %q, want changed", on)
	}

	// A ".." segment that still resolves inside the tree is legal.
	if err := repo.WriteWorktreeFile(ctx, "sub/../ok2.txt", []byte("y\n")); err != nil {
		t.Fatalf("write sub/../ok2.txt (resolves in-tree): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ok2.txt")); err != nil {
		t.Fatalf("ok2.txt not written at tree root: %v", err)
	}
}

func TestWorktreeFileRejectsEscapingPaths(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	escapes := []string{
		"..",
		"../evil.txt",
		"../../evil.txt",
		"a/../../evil.txt",
		"a/b/../../../evil.txt",
	}
	for _, p := range escapes {
		if err := repo.WriteWorktreeFile(ctx, p, []byte("boom")); err == nil {
			t.Errorf("WriteWorktreeFile(%q) succeeded, want escape rejection", p)
		}
		if _, err := repo.ReadWorktreeFile(ctx, p); err == nil {
			t.Errorf("ReadWorktreeFile(%q) succeeded, want escape rejection", p)
		}
	}
	// Nothing may have landed beside the repo root.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("escaping write landed outside the working tree (stat err = %v)", err)
	}
}
