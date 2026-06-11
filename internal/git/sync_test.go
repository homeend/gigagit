package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// gitIn runs a raw git command in dir for test setup.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newClonePair creates a bare remote with one commit on main, then clones it.
func newClonePair(t *testing.T) (string, gitexec.Runner) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")

	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	return clone, gitexec.NewExecRunner("git", clone, observ.NewRing(50))
}

func TestFetchAndPullFFOnly(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}

	if err := repo.Fetch(context.Background(), "origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := repo.PullFFOnly(context.Background(), "origin", "main"); err != nil {
		t.Fatalf("pull ff-only: %v", err)
	}
	_ = clone
}

func TestPushPropagatesCommit(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(clone, "f.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "v2", true); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.Push(context.Background(), "origin", "main", false); err != nil {
		t.Fatalf("push: %v", err)
	}
}
