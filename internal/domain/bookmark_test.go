package domain

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/bookmark"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func bmSvc(t *testing.T) (*Service, *gitexec.FakeRunner) {
	t.Helper()
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetBookmarkStore(bookmark.NewFileStore(t.TempDir()))
	return svc, f
}

func TestBookmarkAddFillsCommittedSHA(t *testing.T) {
	svc, f := bmSvc(t)
	f.SetResponse("git rev-parse blob", gitexec.Result{Stdout: "abc123sha\n"})
	b, err := svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: "c0ffee", Path: "a/b.go",
	})
	if err != nil {
		t.Fatalf("BookmarkAdd: %v", err)
	}
	if b.SHA != "abc123sha" {
		t.Fatalf("committed SHA not filled via BlobSHA: %+v", b)
	}
	if b.ID == "" {
		t.Fatalf("no id assigned")
	}
}

func TestBookmarkBytesCommittedUsesCatFile(t *testing.T) {
	svc, f := bmSvc(t)
	f.SetResponse("git cat-file blob", gitexec.Result{Stdout: "frozen\n"})
	got, err := svc.BookmarkBytes(context.Background(), model.Bookmark{
		State: model.StateCommitted, SHA: "abc123", Path: "a/b.go",
	})
	if err != nil || string(got) != "frozen\n" {
		t.Fatalf("BookmarkBytes committed = %q err %v", got, err)
	}
	if !sawArg(f, "git cat-file blob", "abc123") {
		t.Fatalf("expected cat-file of abc123, calls: %+v", f.Calls)
	}
}

func TestBookmarkBytesStagedUsesShowInDir(t *testing.T) {
	svc, f := bmSvc(t)
	f.SetResponse("git -C show", gitexec.Result{Stdout: "idx\n"})
	if _, err := svc.BookmarkBytes(context.Background(), model.Bookmark{
		State: model.StateStaged, Worktree: "/wt", Path: "a/b.go",
	}); err != nil {
		t.Fatal(err)
	}
	if !sawArg(f, "git -C show", ":a/b.go") || !sawArg(f, "git -C show", "/wt") {
		t.Fatalf("expected `git -C /wt show :a/b.go`, calls: %+v", f.Calls)
	}
}

func TestBookmarkListAndRemove(t *testing.T) {
	svc, _ := bmSvc(t)
	b, _ := svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateUnstaged, Worktree: "/wt", Path: "p.go",
	})
	if list, _ := svc.BookmarkList(context.Background(), 0, 10); len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}
	if err := svc.BookmarkRemove(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.BookmarkList(context.Background(), 0, 10); len(list) != 0 {
		t.Fatalf("after remove len = %d", len(list))
	}
}

func TestBookmarkAddCommitSkipsBlobSHA(t *testing.T) {
	svc, f := bmSvc(t) // FakeRunner has NO rev-parse response; calling it would not fill SHA
	b, err := svc.BookmarkAdd(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: "c0ffee", Path: "",
	})
	if err != nil {
		t.Fatalf("BookmarkAdd(commit): %v", err)
	}
	if b.SHA != "" {
		t.Fatalf("commit bookmark must carry empty SHA, got %q", b.SHA)
	}
	if sawArg(f, "git rev-parse", "c0ffee") {
		t.Fatalf("BlobSHA must not be called for a commit bookmark: %+v", f.Calls)
	}
	if b.ID == "" {
		t.Fatal("no id assigned")
	}
}

func TestBookmarkBytesCommitErrors(t *testing.T) {
	svc, f := bmSvc(t)
	_, err := svc.BookmarkBytes(context.Background(), model.Bookmark{
		State: model.StateCommitted, Commit: "c0ffee", Path: "", // commit bookmark
	})
	if err == nil {
		t.Fatal("BookmarkBytes of a commit bookmark must error")
	}
	if len(f.Calls) != 0 {
		t.Fatalf("must not shell out for a commit bookmark: %+v", f.Calls)
	}
}
