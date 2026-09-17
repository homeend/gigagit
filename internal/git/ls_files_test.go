// internal/git/ls_files_test.go
package git

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestLsFiles(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git ls-files", gitexec.Result{Stdout: "a.go\x00dir/b — c.txt\x00"})
	r := &Repo{Runner: fr}
	got, err := r.LsFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "dir/b — c.txt"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %q want %q", got, want)
	}
	// argv must use -z (raw paths, no quoting)
	var argv []string
	for _, c := range fr.Calls {
		if c.Name == "git ls-files" {
			argv = c.Argv
		}
	}
	found := false
	for _, a := range argv {
		if a == "-z" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ls-files must pass -z; argv=%v", argv)
	}
}

// With a pathspec the listing answers the narrower "which of THESE does the
// index hold". `--` must separate it, so a path that looks like an option
// cannot be read as one.
func TestLsFilesPathspec(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git ls-files", gitexec.Result{Stdout: "a.go\x00"})
	r := &Repo{Runner: fr}
	got, err := r.LsFiles(context.Background(), "a.go", "--not-an-option")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("got %q, want [a.go]", got)
	}
	var argv []string
	for _, c := range fr.Calls {
		if c.Name == "git ls-files" {
			argv = c.Argv
		}
	}
	want := []string{"ls-files", "-z", "--", "a.go", "--not-an-option"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}
