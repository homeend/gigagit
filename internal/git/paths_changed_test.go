package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestStatusPathsAndDiffNamesBetween(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "a\n")
	write("b.go", "b\n")
	write("c.go", "c\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "abc")
	gitRun(t, dir, "tag", "first")
	write("a.go", "a2\n")
	gitRun(t, dir, "add", "a.go")
	gitRun(t, dir, "commit", "-q", "-m", "a2")
	write("b.go", "b2\n")  // unstaged edit
	write("new.go", "n\n") // untracked
	if err := os.Remove(filepath.Join(dir, "c.go")); err != nil {
		t.Fatal(err)
	}
	got, err := repo.StatusPaths(ctx, []string{"a.go", "b.go", "c.go", "new.go"})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"b.go", "c.go", "new.go"}) {
		t.Fatalf("StatusPaths = %v, want [b.go c.go new.go]", got)
	}
	diff, err := repo.DiffNamesBetween(ctx, "first", "HEAD", []string{"a.go", "b.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(diff, []string{"a.go"}) {
		t.Fatalf("DiffNamesBetween = %v, want [a.go]", diff)
	}
}

func TestPathVerbsEmptyNeverInvokeGit(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	repo := &Repo{Runner: fr}
	if got, err := repo.StatusPaths(context.Background(), nil); got != nil || err != nil {
		t.Fatalf("StatusPaths(nil) = %v %v", got, err)
	}
	if got, err := repo.DiffNamesBetween(context.Background(), "a", "b", nil); got != nil || err != nil {
		t.Fatalf("DiffNamesBetween(nil) = %v %v", got, err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("git was invoked: %v", fr.Calls)
	}
}
