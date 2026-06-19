package domain

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/shelf"
)

func shelfSvc(t *testing.T) (*Service, *gitexec.FakeRunner) {
	t.Helper()
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetShelfStore(shelf.NewFileStore(t.TempDir()))
	return svc, f
}

func TestShelfAddAndBlobRoundTrip(t *testing.T) {
	svc, f := shelfSvc(t)
	f.SetResponse("git show", gitexec.Result{Stdout: "commit-bytes\n"})

	e, err := svc.ShelfAdd(context.Background(),
		model.FileAddress{State: model.StateCommitted, Commit: "abc", Path: "a/b.go"}, "")
	if err != nil {
		t.Fatalf("ShelfAdd: %v", err)
	}
	if e.Bucket != "default" || e.Origin.Path != "a/b.go" {
		t.Fatalf("entry = %+v", e)
	}
	got, err := svc.ShelfBlob(context.Background(), e.ID)
	if err != nil {
		t.Fatalf("ShelfBlob: %v", err)
	}
	if string(got) != "commit-bytes\n" {
		t.Fatalf("blob = %q", got)
	}
}

func TestShelfListAndRemove(t *testing.T) {
	svc, f := shelfSvc(t)
	f.SetResponse("git show", gitexec.Result{Stdout: "x\n"})

	e, err := svc.ShelfAdd(context.Background(),
		model.FileAddress{State: model.StateCommitted, Commit: "abc", Path: "p.go"}, "")
	if err != nil {
		t.Fatal(err)
	}
	list, err := svc.ShelfList(context.Background(), "", 0, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, err = %v", list, err)
	}
	if err := svc.ShelfRemove(context.Background(), e.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.ShelfList(context.Background(), "", 0, 10)
	if len(list) != 0 {
		t.Fatalf("after remove, list = %v", list)
	}
}

func TestResolveBytesShelfSourceReadsStoredBlob(t *testing.T) {
	svc, f := shelfSvc(t)
	f.SetResponse("git show", gitexec.Result{Stdout: "frozen\n"})
	e, err := svc.ShelfAdd(context.Background(),
		model.FileAddress{State: model.StateCommitted, Commit: "abc", Path: "p.go"}, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ResolveBytes(context.Background(),
		model.FileRef{Source: model.SourceShelf, Locator: e.ID, Path: "p.go"})
	if err != nil {
		t.Fatalf("ResolveBytes shelf: %v", err)
	}
	if string(got) != "frozen\n" {
		t.Fatalf("got %q", got)
	}
}

func TestShelfDisabledWhenNoStateDir(t *testing.T) {
	// No injected store and no resolvable state dir → shelf disabled.
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("LocalAppData", "")
	old := ShelfStatePath
	ShelfStatePath = ""
	defer func() { ShelfStatePath = old }()

	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	if _, err := svc.ShelfList(context.Background(), "", 0, 10); err != ErrShelfDisabled {
		t.Fatalf("err = %v, want ErrShelfDisabled", err)
	}
}
