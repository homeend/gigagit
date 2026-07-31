package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf"
)

// writeAndCommit overwrites paths (contents keyed by path) and commits them
// as one commit, returning its sha.
func writeAndCommit(t *testing.T, dir, msg string, files map[string]string) string {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", msg)
	return headHash(t, dir)
}

func TestResolveCommitEntryEndpoint(t *testing.T) {
	dir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()
	sha := commitTwoFiles(t, dir)

	// Live sha → EndpointCommit carrying the FULL stored sha.
	ep, err := svc.ResolveCommitEntryEndpoint(ctx, sha, "some-shelf-id")
	if err != nil {
		t.Fatalf("live resolve: %v", err)
	}
	if ep.Kind != model.EndpointCommit || ep.Hash != sha {
		t.Fatalf("live resolve = %+v, want EndpointCommit with full sha %s", ep, sha)
	}

	// Gone sha + shelf id → frozen fallback.
	gone := "0123456789abcdef0123456789abcdef01234567"
	ep, err = svc.ResolveCommitEntryEndpoint(ctx, gone, "entry-1")
	if err != nil {
		t.Fatalf("frozen resolve: %v", err)
	}
	if ep.Kind != model.EndpointShelf || ep.ShelfID != "entry-1" {
		t.Fatalf("frozen resolve = %+v, want EndpointShelf entry-1", ep)
	}

	// Gone sha, no shelf id (a bookmark) → typed error naming the short sha.
	_, err = svc.ResolveCommitEntryEndpoint(ctx, gone, "")
	var cg *CommitGoneError
	if !errors.As(err, &cg) {
		t.Fatalf("bookmark gone: err = %v, want *CommitGoneError", err)
	}
	if cg.SHA != gone {
		t.Fatalf("CommitGoneError.SHA = %q, want the full sha", cg.SHA)
	}
}

func TestCompareFilesShelfShelf(t *testing.T) {
	dir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	// Commit A changes shared.txt + only-a.txt; commit B changes shared.txt
	// (different bytes), same.txt (same bytes as A's version? no — same.txt is
	// identical in both) + only-b.txt.
	shaA := writeAndCommit(t, dir, "A", map[string]string{
		"shared.txt": "from-a\n",
		"same.txt":   "identical\n",
		"only-a.txt": "a\n",
	})
	shaB := writeAndCommit(t, dir, "B", map[string]string{
		"shared.txt": "from-b\n",
		"same.txt":   "identical\n",
		"only-b.txt": "b\n",
	})

	ea, err := svc.ShelfAddCommit(ctx, shaA, "")
	if err != nil {
		t.Fatal(err)
	}
	eb, err := svc.ShelfAddCommit(ctx, shaB, "")
	if err != nil {
		t.Fatal(err)
	}

	files, err := svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: ea.ID},
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: eb.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	// A shelf entry's tar holds only ITS OWN commit's changed paths
	// (commitChangedPaths), not the whole tree. same.txt's content is
	// identical in A and B's working trees, so B's commit against parent A
	// never touches it — it lands only in A's tar, not B's. A's tar:
	// shared.txt+same.txt+only-a.txt. B's tar: shared.txt+only-b.txt.
	want := map[string]string{
		"shared.txt": "M", // in both tars, different bytes
		"same.txt":   "D", // only in A's (left) tar — B never changed it
		"only-a.txt": "D", // only in left tar
		"only-b.txt": "A", // only in right tar
	}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want exactly %v", got, want)
	}
	for p, s := range want {
		if got[p] != s {
			t.Errorf("%s = %q, want %q", p, got[p], s)
		}
	}
}

func TestCompareFilesShelfVsCommit(t *testing.T) {
	dir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	shaA := writeAndCommit(t, dir, "A", map[string]string{
		"changed.txt": "old\n",
		"gone.txt":    "will vanish\n",
	})
	ea, err := svc.ShelfAddCommit(ctx, shaA, "")
	if err != nil {
		t.Fatal(err)
	}
	// The newer commit rewrites changed.txt and deletes gone.txt.
	gitRun(t, dir, "rm", "gone.txt")
	shaB := writeAndCommit(t, dir, "B", map[string]string{"changed.txt": "new\n"})

	// shelf (left/older) vs commit (right/newer): scoped to the shelf members.
	files, err := svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: ea.ID},
		model.Endpoint{Kind: model.EndpointCommit, Hash: shaB})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	if got["changed.txt"] != "M" || got["gone.txt"] != "D" || len(got) != 2 {
		t.Fatalf("files = %v, want changed.txt=M gone.txt=D only", got)
	}

	// Reversed order (commit older, shelf newer): the vanished member reads as added.
	files, err = svc.CompareFiles(ctx,
		model.Endpoint{Kind: model.EndpointCommit, Hash: shaB},
		model.Endpoint{Kind: model.EndpointShelf, ShelfID: ea.ID})
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]string{}
	for _, f := range files {
		got[f.Path] = f.Status
	}
	if got["changed.txt"] != "M" || got["gone.txt"] != "A" || len(got) != 2 {
		t.Fatalf("reversed files = %v, want changed.txt=M gone.txt=A only", got)
	}
}
