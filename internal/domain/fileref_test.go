package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// sawArg reports whether any call named name carried argv element arg.
func sawArg(f *gitexec.FakeRunner, name, arg string) bool {
	for _, c := range f.Calls {
		if c.Name != name {
			continue
		}
		for _, a := range c.Argv {
			if a == arg {
				return true
			}
		}
	}
	return false
}

func TestResolveBytesCommitUsesShow(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{Stdout: "file-at-commit\n"})
	svc := New(&git.Repo{Runner: f})

	got, err := svc.ResolveBytes(context.Background(), model.FileRef{
		Source: model.SourceCommit, Locator: "abc123", Path: "a/b.go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "file-at-commit\n" {
		t.Fatalf("got %q", got)
	}
	if !sawArg(f, "git show", "abc123:a/b.go") {
		t.Fatalf("expected show of abc123:a/b.go, calls: %+v", f.Calls)
	}
}

func TestResolveBytesStagedShowsIndexBlob(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{Stdout: "index-blob\n"})
	svc := New(&git.Repo{Runner: f})

	if _, err := svc.ResolveBytes(context.Background(), model.FileRef{
		Source: model.SourceStaged, Path: "a/b.go",
	}); err != nil {
		t.Fatal(err)
	}
	if !sawArg(f, "git show", ":a/b.go") {
		t.Fatalf("expected show of :a/b.go (index), calls: %+v", f.Calls)
	}
}
