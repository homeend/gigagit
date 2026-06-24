package git

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestRepoCreateTag(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	head := gitOutIn(t, dir, "rev-parse", "HEAD")

	if err := repo.CreateTag(context.Background(), "v1.0.0", head, "", false); err != nil {
		t.Fatalf("lightweight: %v", err)
	}
	if err := repo.CreateTag(context.Background(), "v2.0.0", head, "release two", false); err != nil {
		t.Fatalf("annotated: %v", err)
	}
	if typ := gitOutIn(t, dir, "cat-file", "-t", "v1.0.0"); typ != "commit" {
		t.Fatalf("v1.0.0 type = %q, want commit (lightweight)", typ)
	}
	if typ := gitOutIn(t, dir, "cat-file", "-t", "v2.0.0"); typ != "tag" {
		t.Fatalf("v2.0.0 type = %q, want tag (annotated)", typ)
	}
}

func TestCreateTagForceArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git tag", gitexec.Result{})
	repo := &Repo{Runner: f}
	if err := repo.CreateTag(context.Background(), "v1.0.0", "abc1234", "rel", true); err != nil {
		t.Fatalf("CreateTag force: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git tag" {
			argv = c.Argv
		}
	}
	want := []string{"tag", "-a", "-m", "rel", "-f", "v1.0.0", "abc1234"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

func TestCreateTagForceAnnotatesExisting(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c1")
	head := gitOutIn(t, dir, "rev-parse", "HEAD")
	if err := repo.CreateTag(context.Background(), "v1.0.0", head, "", false); err != nil {
		t.Fatalf("lightweight: %v", err)
	}
	if typ := gitOutIn(t, dir, "cat-file", "-t", "v1.0.0"); typ != "commit" {
		t.Fatalf("lightweight tag object type = %q, want commit", typ)
	}
	if err := repo.CreateTag(context.Background(), "v1.0.0", head, "now annotated", true); err != nil {
		t.Fatalf("force annotate: %v", err)
	}
	if typ := gitOutIn(t, dir, "cat-file", "-t", "v1.0.0"); typ != "tag" {
		t.Fatalf("after annotate, tag object type = %q, want tag (annotated)", typ)
	}
	if got := gitOutIn(t, dir, "rev-parse", "v1.0.0^{commit}"); got != head {
		t.Fatalf("annotate moved the target: %q != %q", got, head)
	}
}
