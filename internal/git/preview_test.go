package git

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestCountLeftRightArgvAndParse(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-list --left-right --count", gitexec.Result{Stdout: "2\t5\n"})
	r := &Repo{Runner: f}
	l, rr, err := r.CountLeftRight(context.Background(), "aaaa", "bbbb")
	if err != nil || l != 2 || rr != 5 {
		t.Fatalf("got %d %d %v", l, rr, err)
	}
	want := []string{"rev-list", "--left-right", "--count", "aaaa...bbbb"}
	if got := f.Calls[0].Argv; !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestCountLeftRightBadOutput(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-list --left-right --count", gitexec.Result{Stdout: "garbage\n"})
	if _, _, err := (&Repo{Runner: f}).CountLeftRight(context.Background(), "a", "b"); err == nil {
		t.Fatal("unparseable output must error")
	}
}

func TestDiffNameOnlyRangeArgvAndSplit(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff --name-only (range)", gitexec.Result{Stdout: "a.go\x00dir/b.txt\x00"})
	r := &Repo{Runner: f}
	paths, err := r.DiffNameOnlyRange(context.Background(), "base", "tip")
	if err != nil || len(paths) != 2 || paths[1] != "dir/b.txt" {
		t.Fatalf("paths = %v, %v", paths, err)
	}
	want := []string{"diff", "--name-only", "-z", "base...tip"}
	if got := f.Calls[0].Argv; !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v, want %v", got, want)
	}
	f.SetResponse("git diff --name-only (range)", gitexec.Result{Stdout: ""})
	if paths, _ := r.DiffNameOnlyRange(context.Background(), "base", "tip"); len(paths) != 0 {
		t.Fatalf("empty diff must yield no paths, got %v", paths)
	}
}
