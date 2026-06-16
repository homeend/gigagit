package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteWorktreeFile(t *testing.T) {
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
}
