package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// newTestRepo creates a temp git repo with one commit and returns its path and
// a runner scoped to it.
func newTestRepo(t *testing.T) (string, gitexec.Runner) {
	t.Helper()
	dir := gittest.BasicRepo(t, "hello\n")
	return dir, gitexec.NewExecRunner("git", dir, observ.NewRing(50))
}

func TestRepoStatusCleanThenDirty(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// TestBranchTagNameCollisionStaysUnambiguous reproduces the worktree-from-tag
// bug: when a branch and a tag share a name (e.g. a tag v8.0.0-rc.3 and a branch
// created at it), git's %(refname:short) disambiguates the names to
// "heads/v8.0.0-rc.3" / "tags/v8.0.0-rc.3" — forms that break `git switch` and
// worktree matching. Branches() and Tags() must report the bare branch/tag name.
func TestBranchTagNameCollisionStaysUnambiguous(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	const name = "v8.0.0-rc.3"
	git("tag", name)            // refs/tags/v8.0.0-rc.3
	git("branch", name, "HEAD") // refs/heads/v8.0.0-rc.3 — now ambiguous with the tag

	repo := &Repo{Runner: runner}

	bs, err := repo.Branches(context.Background())
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	var found bool
	for _, b := range bs {
		if b.Name == "heads/"+name {
			t.Fatalf("branch name disambiguated to %q, want bare %q", b.Name, name)
		}
		if b.Name == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("bare branch %q not found in %+v", name, bs)
	}

	ts, err := repo.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	found = false
	for _, tg := range ts {
		if tg.Name == "tags/"+name {
			t.Fatalf("tag name disambiguated to %q, want bare %q", tg.Name, name)
		}
		if tg.Name == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("bare tag %q not found in %+v", name, ts)
	}
}

func TestBranchesIncludeCommitterDate(t *testing.T) {
	t.Parallel()
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

func TestRepoRemoteBranches(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	// Fabricate remote-tracking refs plus the default-branch symref.
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	git("update-ref", "refs/remotes/origin/feature/x", "HEAD")
	git("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	repo := &Repo{Runner: runner}
	rbs, err := repo.RemoteBranches(context.Background())
	if err != nil {
		t.Fatalf("RemoteBranches: %v", err)
	}
	got := map[string]model.RemoteBranch{}
	for _, rb := range rbs {
		got[rb.Name] = rb
	}
	if _, ok := got["origin/HEAD"]; ok {
		t.Fatalf("origin/HEAD symref should be filtered out: %+v", rbs)
	}
	main, ok := got["origin/main"]
	if !ok || main.Remote != "origin" || main.Branch != "main" || main.UnixTime == 0 {
		t.Fatalf("origin/main = %+v (ok=%v)", main, ok)
	}
	if feat, ok := got["origin/feature/x"]; !ok || feat.Branch != "feature/x" {
		t.Fatalf("origin/feature/x = %+v (ok=%v)", feat, ok)
	}
}

func TestRepoWorktrees(t *testing.T) {
	t.Parallel()
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
