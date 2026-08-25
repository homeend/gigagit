package engine

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

func pushTagsFakeRepo() (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git remote", gitexec.Result{Stdout: "origin\n"})
	f.SetResponse("git push (tags)", gitexec.Result{}) // succeed
	return &git.Repo{Runner: f}, f
}

// pushedTagsArgv returns the argv of the `git push (tags)` call, if any.
func pushedTagsArgv(f *gitexec.FakeRunner) ([]string, bool) {
	for _, c := range f.Calls {
		if c.Name == "git push (tags)" {
			return c.Argv, true
		}
	}
	return nil, false
}

func TestPushTagsOp(t *testing.T) {
	t.Parallel()
	repo, f := pushTagsFakeRepo()
	res, err := PushTags{Remote: "origin", Names: []string{"a", "b"}}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed:true")
	}
	if res.Summary != "pushed tags" {
		t.Fatalf("Summary = %q, want %q", res.Summary, "pushed tags")
	}
	argv, ok := pushedTagsArgv(f)
	if !ok {
		t.Fatal("expected git push (tags) call")
	}
	want := []string{"push", "origin", "refs/tags/a", "refs/tags/b"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q (full: %v)", i, argv[i], want[i], argv)
		}
	}
}

func TestPushTagsOpEmpty(t *testing.T) {
	t.Parallel()
	repo, f := pushTagsFakeRepo()
	res, err := PushTags{Remote: "origin", Names: nil}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Changed {
		t.Fatal("empty Names must be Changed:false")
	}
	if _, ok := pushedTagsArgv(f); ok {
		t.Fatal("empty Names must not invoke git push (tags)")
	}
}

func TestPushTagsOpAutoRemote(t *testing.T) {
	t.Parallel()
	// Remote "" with a single configured remote → resolves automatically.
	repo, f := pushTagsFakeRepo()
	res, err := PushTags{Remote: "", Names: []string{"v1.0.0"}}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("auto-remote: unexpected error: %v", err)
	}
	if !res.Changed {
		t.Fatal("auto-remote: expected Changed:true")
	}
	argv, ok := pushedTagsArgv(f)
	if !ok {
		t.Fatal("auto-remote: expected git push (tags) call")
	}
	if len(argv) < 2 || argv[1] != "origin" {
		t.Fatalf("auto-remote: argv[1] = %q, want origin (full: %v)", argv[1], argv)
	}
}
