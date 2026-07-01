package git

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestParentCountArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-list (parent count)", gitexec.Result{Stdout: "abc def\n"})
	r := &Repo{Runner: f}
	n, err := r.ParentCount(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("parents = %d, want 1", n)
	}
	got := strings.Join(f.Calls[0].Argv, " ")
	if got != "rev-list --parents --max-count=1 abc" {
		t.Fatalf("argv = %q", got)
	}
}

func TestParentCountRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}

	// root commit: 0 parents (initial commit from newTestRepo)
	if n, err := r.ParentCount(context.Background(), "HEAD~0"); err != nil || n != 0 {
		t.Fatalf("root ParentCount = %d, %v; want 0, nil", n, err)
	}
	// normal commit: 1 parent
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	if n, err := r.ParentCount(context.Background(), "HEAD"); err != nil || n != 1 {
		t.Fatalf("normal ParentCount = %d, %v; want 1, nil", n, err)
	}
	// merge commit: 2 parents
	gitIn(t, dir, "checkout", "-b", "topic", "HEAD~1")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "topic")
	gitIn(t, dir, "checkout", "-")           // back to the default branch
	gitIn(t, dir, "merge", "--no-ff", "topic", "-m", "merge topic")
	if n, err := r.ParentCount(context.Background(), "HEAD"); err != nil || n != 2 {
		t.Fatalf("merge ParentCount = %d, %v; want 2, nil", n, err)
	}
}

func TestFormatPatchArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git format-patch", gitexec.Result{Stdout: "From ...\n"})
	r := &Repo{Runner: f}

	if _, err := r.FormatPatch(context.Background(), "abc123"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "format-patch -1 --binary --stdout abc123" {
		t.Fatalf("whole-commit argv = %q", got)
	}

	f2 := gitexec.NewFakeRunner()
	f2.SetResponse("git format-patch", gitexec.Result{Stdout: "From ...\n"})
	r2 := &Repo{Runner: f2}
	if _, err := r2.FormatPatch(context.Background(), "abc123", "dir/file.go"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f2.Calls[0].Argv, " "); got != "format-patch -1 --binary --stdout abc123 -- dir/file.go" {
		t.Fatalf("file-scoped argv = %q", got)
	}
}

func TestFormatPatchRealRepoScopesToPath(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "base")
	writeFile(t, dir, "foo.go", "a\nb\nc\n")
	writeFile(t, dir, "bar.txt", "x\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add foo and bar")

	// whole-commit patch touches both files; a valid mailbox patch starts "From "
	whole, err := r.FormatPatch(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(whole), "From ") {
		t.Fatalf("patch does not start with mailbox header: %q", string(whole)[:20])
	}
	if !strings.Contains(string(whole), "foo.go") || !strings.Contains(string(whole), "bar.txt") {
		t.Fatal("whole-commit patch should mention both files")
	}
	// path-scoped patch mentions only foo.go
	scoped, err := r.FormatPatch(context.Background(), "HEAD", "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scoped), "foo.go") || strings.Contains(string(scoped), "bar.txt") {
		t.Fatalf("scoped patch should mention only foo.go:\n%s", scoped)
	}
}
