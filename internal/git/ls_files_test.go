// internal/git/ls_files_test.go
package git

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestLsFiles(t *testing.T) {
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
