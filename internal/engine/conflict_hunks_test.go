package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/hunkpick"
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

// TestResolveConflictHunksRealRoundTrip exercises the real glue: parse a genuine
// git conflict file with hunkpick, assemble a line-by-line resolution (incoming
// then current), and run it through ResolveConflictHunks against the real repo.
func TestResolveConflictHunksRealRoundTrip(t *testing.T) {
	dir, repo := newConflictRepo(t)
	ctx := context.Background()

	raw, err := repo.ReadWorktreeFile(ctx, "uu.txt")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := hunkpick.ParseConflict(raw)
	if err != nil {
		t.Fatalf("ParseConflict on real markers: %v", err)
	}
	bs := doc.Blocks()
	if len(bs) != 1 {
		t.Fatalf("got %d regions from real conflict, want 1", len(bs))
	}
	// line-by-line: take incoming line 0 ("theirs") then current line 0 ("ours")
	bs[0].Mode = hunkpick.LineByLine
	bs[0].ToggleLine(hunkpick.Incoming, 0)
	bs[0].ToggleLine(hunkpick.Current, 0)
	out, ok := doc.Resolved()
	if !ok {
		t.Fatal("Resolved ok=false")
	}
	if string(out) != "theirs\nours\n" {
		t.Fatalf("assembled = %q, want theirs/ours", out)
	}

	if _, err := (ResolveConflictHunks{Path: "uu.txt", Content: out}).
		Run(ctx, OpDeps{Repo: repo, Events: make(chan Event, 16)}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "uu.txt")); string(b) != "theirs\nours\n" {
		t.Fatalf("uu.txt on disk = %q", b)
	}
	st, _ := repo.Status(ctx)
	for _, f := range st.Conflicts() {
		if f.Path == "uu.txt" {
			t.Fatal("uu.txt should be resolved + staged")
		}
	}
}
