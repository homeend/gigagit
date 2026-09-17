package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

// TreePaths is the pathspec-limited twin of TreeFiles: it must answer only
// about the paths asked for, against a real tree.
func TestTreePathsLimitsToThePathspec(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // one commit: README.md
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	const spaced = "a file.txt"
	if err := os.WriteFile(filepath.Join(dir, spaced), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", spaced)
	gitRun(t, dir, "commit", "-m", "spaced")

	got, err := repo.TreePaths(ctx, "HEAD", []string{spaced, "never.txt"})
	if err != nil {
		t.Fatalf("TreePaths: %v", err)
	}
	if len(got) != 1 || got[0] != spaced {
		t.Fatalf("TreePaths = %v, want exactly [%q] (README.md is in the tree but was not asked about)", got, spaced)
	}
}

// An empty pathspec must NOT reach git: `ls-tree … --` with no paths lists the
// whole tree, the opposite of what "no paths" means.
func TestTreePathsEmptyNeverInvokesGit(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	r := &Repo{Runner: fr}
	got, err := r.TreePaths(context.Background(), "deadbeef", nil)
	if err != nil || got != nil {
		t.Fatalf("TreePaths(_, nil) = %v, %v; want nil, nil", got, err)
	}
	if len(fr.Calls) != 0 {
		t.Fatalf("an empty pathspec must not invoke git, got %v", fr.Calls)
	}
}

// The same pathspec-magic hazard on the tree side: a path beginning with ':'
// passed raw matches nothing and exits 0, so the commit lane would call a file
// that IS in the tree absent. Verified on git 2.43.0 that :(literal) fixes it
// for `ls-tree -r` as well as for `ls-files`.
func TestTreePathsFindsAColonPrefixedPath(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t) // one commit: README.md
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	const colon = ":colon.txt"
	if err := os.WriteFile(filepath.Join(dir, colon), []byte("x\n"), 0o644); err != nil {
		// Windows forbids ':' in a filename; the bug it guards cannot occur there.
		t.Skipf("cannot create %q on this platform (%v) — the pathspec-magic hazard is POSIX-only", colon, err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "a colon-prefixed name")

	got, err := repo.TreePaths(ctx, "HEAD", []string{colon})
	if err != nil {
		t.Fatalf("TreePaths: %v", err)
	}
	if len(got) != 1 || got[0] != colon {
		t.Fatalf("TreePaths = %v, want [%q] — a colon path must not be eaten as pathspec magic", got, colon)
	}
	// The ordinary path must still match.
	plain, err := repo.TreePaths(ctx, "HEAD", []string{"README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 1 || plain[0] != "README.md" {
		t.Fatalf("TreePaths(README.md) = %v, want [README.md]", plain)
	}
}

// The argv must carry -z, `--`, and a :(literal) wrapper on every element.
func TestTreePathsArgv(t *testing.T) {
	t.Parallel()
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git ls-tree (tree paths)", gitexec.Result{Stdout: "a.go\x00"})
	r := &Repo{Runner: fr}
	if _, err := r.TreePaths(context.Background(), "deadbeef", []string{"a.go", ":colon.txt"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"ls-tree", "-r", "--name-only", "-z", "deadbeef", "--", ":(literal)a.go", ":(literal):colon.txt"}
	var argv []string
	for _, c := range fr.Calls {
		if c.Name == "git ls-tree (tree paths)" {
			argv = c.Argv
		}
	}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv = %v, want %v", argv, want)
		}
	}
}
