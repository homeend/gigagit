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

// newTestRepo creates a temp git repo with one commit and returns its path and
// a runner scoped to it.
func newTestRepo(t *testing.T) (string, gitexec.Runner) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
	return dir, gitexec.NewExecRunner("git", dir, observ.NewRing(50))
}

func TestRepoStatusCleanThenDirty(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	st, err := repo.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Branch != "main" {
		t.Errorf("branch = %q, want main", st.Branch)
	}
	if c := st.Counts(); c.Staged+c.Unstaged+c.Untracked+c.Conflicted != 0 {
		t.Errorf("expected clean tree, got %+v", c)
	}

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, _ = repo.Status(context.Background())
	if st.Counts().Untracked != 1 {
		t.Errorf("expected 1 untracked, got %+v", st.Counts())
	}
}

func TestRepoBranches(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	bs, err := repo.Branches(context.Background())
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
	if len(bs) != 1 || bs[0].Name != "main" || !bs[0].IsHead {
		t.Fatalf("branches = %+v, want one head branch 'main'", bs)
	}
}

func TestBranchesIncludeCommitterDate(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	bs, err := repo.Branches(context.Background())
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(bs) == 0 {
		t.Fatal("expected at least one branch")
	}
	if bs[0].UnixTime == 0 {
		t.Fatalf("expected nonzero UnixTime, got %+v", bs[0])
	}
}

func TestRepoWorktrees(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	wts, err := repo.Worktrees(context.Background())
	if err != nil {
		t.Fatalf("worktrees: %v", err)
	}
	if len(wts) != 1 || wts[0].Branch != "main" {
		t.Fatalf("worktrees = %+v, want one on 'main'", wts)
	}
}
