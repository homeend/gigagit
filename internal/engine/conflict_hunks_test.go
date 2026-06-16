package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConflictHunksWritesAndStages(t *testing.T) {
	dir, repo := newConflictRepo(t) // uu.txt is UU (ours/theirs), md.txt is DU
	ctx := context.Background()

	// Assemble an interleaved resolution for uu.txt and write it through the op.
	content := []byte("ours\ntheirs\n")
	_, err := ResolveConflictHunks{Path: "uu.txt", Content: content}.
		Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "ours\ntheirs\n" {
		t.Fatalf("uu.txt on disk = %q", b)
	}
	st, _ := repo.Status(ctx)
	for _, f := range st.Conflicts() {
		if f.Path == "uu.txt" {
			t.Fatal("uu.txt should no longer be unmerged after resolve")
		}
	}
}
