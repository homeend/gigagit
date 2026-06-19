package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitAllAndCurrentBranch(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "second", true, false); err != nil {
		t.Fatalf("commit: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if c := st.Counts(); c.Staged+c.Unstaged != 0 {
		t.Fatalf("expected clean tree after commit -a, got %+v", c)
	}
}

func TestCreateBranchAndSwitch(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := repo.CreateBranch(context.Background(), "feature", ""); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := repo.Switch(context.Background(), "feature"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	cur, err := repo.CurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature", cur)
	}
}

func TestRenameBranch(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := repo.CreateBranch(context.Background(), "old", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.RenameBranch(context.Background(), "old", "renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if ok, _ := repo.LocalBranchExists(context.Background(), "renamed"); !ok {
		t.Fatalf("renamed branch missing after rename")
	}
	if ok, _ := repo.LocalBranchExists(context.Background(), "old"); ok {
		t.Fatalf("old branch still present after rename")
	}
	// git refuses renaming onto an existing branch name.
	if err := repo.RenameBranch(context.Background(), "renamed", "main"); err == nil {
		t.Fatalf("want error renaming onto existing branch main")
	}
}

func TestCreateBranchFromStartPoint(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// Pin "base" at the initial commit, then advance main so HEAD != base.
	if err := repo.CreateBranch(context.Background(), "base", ""); err != nil {
		t.Fatalf("create base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "second", true, false); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := repo.CreateBranch(context.Background(), "from-base", "base"); err != nil {
		t.Fatalf("create from start point: %v", err)
	}
	if got, want := revParse(t, dir, "from-base"), revParse(t, dir, "base"); got != want {
		t.Fatalf("from-base = %s, want tip of base %s", got, want)
	}
	if revParse(t, dir, "from-base") == revParse(t, dir, "main") {
		t.Fatal("from-base must not point at advanced main")
	}
}

func TestCurrentBranchDetachedReturnsEmpty(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "second")
	gitIn(t, dir, "checkout", "HEAD~1") // detach
	cur, err := repo.CurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("detached HEAD should not error: %v", err)
	}
	if cur != "" {
		t.Fatalf("current branch = %q, want empty on detached HEAD", cur)
	}
}

// gitExec runs a git command in dir, failing the test on error.
func gitExec(t *testing.T, dir string, a ...string) string {
	t.Helper()
	c := exec.Command("git", a...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", a, err, out)
	}
	return string(out)
}

func TestLocalBranchExists(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if ok, err := repo.LocalBranchExists(context.Background(), "main"); err != nil || !ok {
		t.Fatalf("main exists: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.LocalBranchExists(context.Background(), "nope"); err != nil || ok {
		t.Fatalf("nope: ok=%v err=%v (want false,nil)", ok, err)
	}
}

func TestIsAncestor(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitExec(t, dir, "commit", "--allow-empty", "-m", "c2")
	if ok, err := repo.IsAncestor(context.Background(), "HEAD~1", "HEAD"); err != nil || !ok {
		t.Fatalf("HEAD~1 ancestor of HEAD: ok=%v err=%v", ok, err)
	}
	if ok, err := repo.IsAncestor(context.Background(), "HEAD", "HEAD~1"); err != nil || ok {
		t.Fatalf("HEAD not ancestor of HEAD~1: ok=%v err=%v (want false,nil)", ok, err)
	}
}

// configFakeRemote makes refs/remotes/origin/* genuine remote-tracking branches
// (a configured remote with a fetch refspec) so `git branch --track` accepts
// origin/* as a tracking start point — mirroring a real fetched repo.
func configFakeRemote(t *testing.T, dir string) {
	t.Helper()
	gitExec(t, dir, "config", "remote.origin.url", "file://"+dir)
	gitExec(t, dir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
}

func TestCreateTrackingBranch(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	configFakeRemote(t, dir)
	gitExec(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	if err := repo.CreateTrackingBranch(context.Background(), "foo", "origin/foo"); err != nil {
		t.Fatalf("CreateTrackingBranch: %v", err)
	}
	up := strings.TrimSpace(gitExec(t, dir, "for-each-ref", "--format=%(upstream:short)", "refs/heads/foo"))
	if up != "origin/foo" {
		t.Fatalf("upstream = %q, want origin/foo", up)
	}
}
