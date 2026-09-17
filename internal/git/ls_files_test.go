// internal/git/ls_files_test.go
package git

import (
	"context"
	"os"
	"path/filepath"
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
// cannot be read as one, and every element must be :(literal)-wrapped, so a
// path is matched as a path rather than as a pathspec expression.
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
	want := []string{"ls-files", "-z", "--", ":(literal)a.go", ":(literal)--not-an-option"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}

// A path beginning with ':' is short-form PATHSPEC MAGIC to git, so passing it
// raw matches nothing and exits 0 — a presence probe would silently call a
// tracked file absent. :(literal) is what stops that. Verified on git 2.43.0:
// `git ls-files -- ':colon.txt'` prints nothing; with :(literal) it finds it.
func TestLsFilesFindsAColonPrefixedPath(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // one commit: README.md
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	const colon = ":colon.txt"
	if err := os.WriteFile(filepath.Join(dir, colon), []byte("x\n"), 0o644); err != nil {
		// Windows forbids ':' in a filename; the bug it guards cannot occur there.
		t.Skipf("cannot create %q on this platform (%v) — the pathspec-magic hazard is POSIX-only", colon, err)
	}
	// A glob metacharacter in the name is the milder half of the same bug: a
	// raw pathspec would also match glob.txt and return a SUPERSET.
	const glob = "f[0-9].txt"
	if err := os.WriteFile(filepath.Join(dir, glob), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f9.txt"), []byte("z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "awkward names")

	got, err := repo.LsFiles(ctx, colon, glob)
	if err != nil {
		t.Fatalf("LsFiles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LsFiles = %v, want exactly [%q %q] — a colon path must not be eaten as pathspec magic, and a glob must not drag in f9.txt", got, colon, glob)
	}
	seen := map[string]bool{got[0]: true, got[1]: true}
	if !seen[colon] || !seen[glob] {
		t.Fatalf("LsFiles = %v, want %q and %q", got, colon, glob)
	}
	// The ordinary path must still match.
	plain, err := repo.LsFiles(ctx, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 1 || plain[0] != "README.md" {
		t.Fatalf("LsFiles(README.md) = %v, want [README.md]", plain)
	}
}
