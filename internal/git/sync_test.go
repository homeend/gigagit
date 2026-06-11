package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// revParse returns the commit hash a ref points to, for assertions.
func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s in %s: %v", ref, dir, err)
	}
	return strings.TrimSpace(string(out))
}

func TestFastForwardRefUpdatesNonCheckedOutBranch(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, seed, "checkout", "-b", "dev")
	gitIn(t, seed, "push", "-u", "origin", "dev")

	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	gitIn(t, clone, "branch", "dev", "origin/dev") // local dev at v1, NOT checked out

	// origin advances dev to v2
	gitIn(t, seed, "checkout", "dev")
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v2")
	gitIn(t, seed, "push", "origin", "dev")

	repo := &Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	if err := repo.FastForwardRef(context.Background(), "origin", "dev"); err != nil {
		t.Fatalf("fast-forward-ref: %v", err)
	}
	if got, want := revParse(t, clone, "dev"), revParse(t, clone, "origin/dev"); got != want {
		t.Fatalf("local dev = %s, want origin/dev %s (ref not fast-forwarded)", got, want)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "main" {
		t.Fatalf("current branch = %q, want main (ff-ref must not checkout)", cur)
	}
}

func TestPullFFOnlyRejectsNonFastForward(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	root := filepath.Dir(clone)
	origin := filepath.Join(root, "origin.git")

	// A second clone pushes a remote commit to origin/main.
	other := filepath.Join(root, "other")
	gitIn(t, root, "clone", origin, other)
	gitIn(t, other, "checkout", "main")
	os.WriteFile(filepath.Join(other, "f.txt"), []byte("remote-v2\n"), 0o644)
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-m", "remote-v2")
	gitIn(t, other, "push", "origin", "main")

	// The local clone makes a DIVERGING commit without fetching.
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local-v2\n"), 0o644)
	gitIn(t, clone, "add", ".")
	gitIn(t, clone, "commit", "-m", "local-v2")

	if err := repo.PullFFOnly(context.Background(), "origin", "main"); err == nil {
		t.Fatal("expected ff-only pull to fail on diverged history")
	}
}
