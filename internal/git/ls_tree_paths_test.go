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
