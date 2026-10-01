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

// A marked SET of working-tree files becomes ONE files entry: the tar lists
// exactly the members, each resolves member-wise, and the entry is an archive
// but not a commit (no sha, so no cherry-pick / commit compare).
func TestShelfAddFilesFreezesTheSetAsOneEntry(t *testing.T) {
	t.Parallel()
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()
	for p, c := range map[string]string{"a.go": "package a\n", "sub/b.go": "package b\n"} {
		full := filepath.Join(repoDir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	addrs := []model.FileAddress{
		{State: model.StateUntracked, Worktree: repoDir, Branch: "main", Path: "a.go"},
		{State: model.StateUntracked, Worktree: repoDir, Branch: "main", Path: "sub/b.go"},
	}
	e, err := svc.ShelfAddFiles(ctx, addrs, "WIP on main")
	if err != nil {
		t.Fatalf("ShelfAddFiles: %v", err)
	}
	if e.Kind != model.ShelfKindFiles || !e.IsArchive() || e.IsCommit() {
		t.Fatalf("entry kind = %v (archive=%v commit=%v), want a files archive that is not a commit", e.Kind, e.IsArchive(), e.IsCommit())
	}
	if e.Label != "WIP on main" || e.Origin.Path != "" || e.Origin.Worktree != repoDir || e.Origin.State != model.StateUntracked {
		t.Fatalf("entry = %+v, want the label and a path-less untracked origin in the worktree", e)
	}
	if !strings.HasPrefix(e.ID, "files-unstaged-") {
		t.Fatalf("id = %q, want the files-<source>-<sha8> scheme", e.ID)
	}
	files, err := svc.ShelfCommitFiles(ctx, e.ID)
	if err != nil {
		t.Fatalf("ShelfCommitFiles: %v", err)
	}
	if len(files) != 2 || files[0].Path != "a.go" || files[1].Path != "sub/b.go" {
		t.Fatalf("members = %+v, want a.go + sub/b.go", files)
	}
	got, err := svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: "sub/b.go"})
	if err != nil || string(got) != "package b\n" {
		t.Fatalf("member bytes = %q err=%v", got, err)
	}
	exp, dir, err := svc.ExportShelfEntry(ctx, e)
	if err != nil || len(exp) != 2 || dir == "" {
		t.Fatalf("export = %v dir=%q err=%v", exp, dir, err)
	}
	// Only the ONE entry landed.
	all, err := svc.ShelfList(ctx, "", 0, 0)
	if err != nil || len(all) != 1 {
		t.Fatalf("shelf lists %d entries (err=%v), want exactly 1", len(all), err)
	}
}

// One unreadable member fails the whole set: nothing is stored.
func TestShelfAddFilesIsAtomic(t *testing.T) {
	t.Parallel()
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(repoDir, "ok.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ShelfAddFiles(ctx, []model.FileAddress{
		{State: model.StateUntracked, Worktree: repoDir, Path: "ok.go"},
		{State: model.StateUntracked, Worktree: repoDir, Path: "gone.go"},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "gone.go") {
		t.Fatalf("err = %v, want a failure naming gone.go", err)
	}
	all, _ := svc.ShelfList(ctx, "", 0, 0)
	if len(all) != 0 {
		t.Fatalf("a failed set must store nothing, shelf has %d entries", len(all))
	}
}

// A shelved commit records the commit it froze, never the rev it was named
// by: "HEAD" would read as a different commit once HEAD moves (and showed
// as "commit / commit" for want of a sha).
func TestShelfAddCommitResolvesTheRev(t *testing.T) {
	t.Parallel()
	repoDir, svc := newRealRepo(t)
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	sha := commitTwoFiles(t, repoDir)
	e, err := svc.ShelfAddCommit(context.Background(), "HEAD", "")
	if err != nil {
		t.Fatalf("ShelfAddCommit: %v", err)
	}
	if e.Origin.Commit != sha || strings.Contains(e.ID, "HEAD") {
		t.Fatalf("entry %q origin commit = %q, want %q", e.ID, e.Origin.Commit, sha)
	}
}
