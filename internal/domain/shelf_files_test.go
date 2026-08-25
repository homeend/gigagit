package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/shelf"
)

// commitTwoFiles writes both paths and commits them as ONE commit, returning
// its sha (writeCommit makes one commit per file, which is wrong here).
func commitTwoFiles(t *testing.T, dir string) string {
	t.Helper()
	for p, c := range map[string]string{"top.txt": "top-v1\n", "sub/inner.txt": "inner-v1\n"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "two files")
	return headHash(t, dir)
}

func TestShelfCommitFilesListsTarMembers(t *testing.T) {
	t.Parallel()
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	sha := commitTwoFiles(t, repoDir)
	e, err := svc.ShelfAddCommit(ctx, sha, "")
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}

	files, err := svc.ShelfCommitFiles(ctx, e.ID)
	if err != nil {
		t.Fatalf("ShelfCommitFiles: %v", err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
	}
	if !got["top.txt"] || !got["sub/inner.txt"] || len(got) != 2 {
		t.Fatalf("files = %v, want exactly top.txt + sub/inner.txt", got)
	}

	// A file entry has no member list.
	if err := os.WriteFile(filepath.Join(repoDir, "plain.txt"), []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fe, err := svc.ShelfAdd(ctx, model.FileAddress{State: model.StateUnstaged, Path: "plain.txt"}, "")
	if err != nil {
		t.Fatalf("ShelfAdd: %v", err)
	}
	if _, err := svc.ShelfCommitFiles(ctx, fe.ID); err == nil {
		t.Fatal("ShelfCommitFiles on a file entry must error")
	}
}

func TestResolveBytesShelfCommitMember(t *testing.T) {
	t.Parallel()
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	sha := commitTwoFiles(t, repoDir)
	e, err := svc.ShelfAddCommit(ctx, sha, "")
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	// Move the working tree past the shelved content: the member must stay frozen.
	writeCommit(t, repoDir, "sub/inner.txt", "inner-v2\n", "edit inner")

	got, err := svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: "sub/inner.txt"})
	if err != nil {
		t.Fatalf("ResolveBytes member: %v", err)
	}
	if string(got) != "inner-v1\n" {
		t.Fatalf("member = %q, want the frozen inner-v1", got)
	}

	// A path not in the shelved commit is an error naming the path.
	if _, err := svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: "nope.txt"}); err == nil || !strings.Contains(err.Error(), "nope.txt") {
		t.Fatalf("missing member should error naming the path, got %v", err)
	}

	// Empty path on a commit entry stays the whole tar (backs export).
	tarBytes, err := svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: e.ID})
	if err != nil {
		t.Fatalf("ResolveBytes tar: %v", err)
	}
	blob, err := svc.ShelfBlob(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(tarBytes) != string(blob) {
		t.Fatal("empty-path resolve of a commit entry must stay the raw blob")
	}
}

func TestResolveBytesShelfFileEntryUnchanged(t *testing.T) {
	t.Parallel()
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("file-v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fe, err := svc.ShelfAdd(ctx, model.FileAddress{State: model.StateUnstaged, Path: "f.txt"}, "")
	if err != nil {
		t.Fatalf("ShelfAdd: %v", err)
	}
	// A file entry's ref carries its origin path for display — resolution must
	// stay the whole blob (NOT attempt tar-member extraction).
	got, err := svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: fe.ID, Path: "f.txt"})
	if err != nil {
		t.Fatalf("ResolveBytes file entry: %v", err)
	}
	if string(got) != "file-v1\n" {
		t.Fatalf("file entry = %q, want file-v1", got)
	}
}
