package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// addWorktree creates a linked worktree of dir at a sibling path on a new
// branch and returns its absolute path.
func addWorktree(t *testing.T, dir, branch, name string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(dir), name)
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", branch, wt, "main")
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

// gitIn runs a git command in dir, failing the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/"+branch).Run()
	return err == nil
}

func TestRemoveWorktreeOnlyKeepsBranch(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/keep", "wt-only")

	ch := make(chan Event, 32)
	res, err := RemoveWorktree{Path: wt, Branch: "feature/keep"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{"remove-scope": "worktree-only"}})
	close(ch)
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir still present: %v", statErr)
	}
	if !branchExists(t, dir, "feature/keep") {
		t.Fatal("branch should be kept for worktree-only")
	}
}

func TestRemoveWorktreeAndBranchDeletesBoth(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/both", "wt-both")

	res, err := RemoveWorktree{Path: wt, Branch: "feature/both"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"remove-scope": "worktree-and-branch"}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if branchExists(t, dir, "feature/both") {
		t.Fatal("branch should be deleted for worktree-and-branch")
	}
}

func TestRemoveWorktreeAbortAtScopeDoesNothing(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/abort", "wt-abort")

	res, err := RemoveWorktree{Path: wt, Branch: "feature/abort"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"remove-scope": "abort"}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if res.Changed {
		t.Fatal("abort should not change anything")
	}
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Fatalf("worktree should still exist after abort: %v", statErr)
	}
}

func TestRemoveWorktreeDirtyForced(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/dirty", "wt-dirty")
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RemoveWorktree{Path: wt, Branch: "feature/dirty"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"remove-scope":   "worktree-only",
			"worktree-dirty": "force",
		}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("dirty worktree not removed after force: %v", statErr)
	}
}

func TestRemoveWorktreeDirtyAbortLeavesIt(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/dirty2", "wt-dirty2")
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RemoveWorktree{Path: wt, Branch: "feature/dirty2"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"remove-scope":   "worktree-only",
			"worktree-dirty": "abort",
		}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if res.Changed {
		t.Fatal("aborting the dirty prompt should leave the worktree")
	}
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Fatalf("worktree should still exist: %v", statErr)
	}
}

func TestRemoveWorktreeUnmergedBranchForceDelete(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/unm", "wt-unm")
	gitIn(t, wt, "config", "user.email", "t@t")
	gitIn(t, wt, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(wt, "f.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-m", "unmerged")

	res, err := RemoveWorktree{Path: wt, Branch: "feature/unm"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"remove-scope":    "worktree-and-branch",
			"branch-unmerged": "force-delete",
		}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if branchExists(t, dir, "feature/unm") {
		t.Fatal("unmerged branch should be force-deleted")
	}
}

func TestRemoveWorktreeUnmergedBranchKept(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/unk", "wt-unk")
	gitIn(t, wt, "config", "user.email", "t@t")
	gitIn(t, wt, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(wt, "f.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-m", "unmerged")

	res, err := RemoveWorktree{Path: wt, Branch: "feature/unk"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"remove-scope":    "worktree-and-branch",
			"branch-unmerged": "keep",
		}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if !branchExists(t, dir, "feature/unk") {
		t.Fatal("branch should be kept")
	}
	if !strings.Contains(res.Summary, "kept") {
		t.Fatalf("summary should mention the branch was kept: %q", res.Summary)
	}
}

func TestRemoveWorktreeDetachedOffersNoBranchOption(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-detached")
	gitIn(t, dir, "worktree", "add", "--detach", wt, "main")

	ch := make(chan Event, 32)
	_, err := RemoveWorktree{Path: wt, Branch: ""}.Run(
		context.Background(),
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{"remove-scope": "worktree-only"}})
	close(ch)
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	var opts []string
	for _, e := range drain(ch) {
		if d, ok := e.(DecisionNeeded); ok && d.Request.ID == "remove-scope" {
			opts = d.Request.Options
		}
	}
	want := []string{"worktree-only", "abort"}
	if strings.Join(opts, ",") != strings.Join(want, ",") {
		t.Fatalf("detached scope options = %v, want %v", opts, want)
	}
}

func TestRemoveWorktreeGuardsCurrentWorktree(t *testing.T) {
	dir, repo := newRepo(t)
	_, err := RemoveWorktree{Path: dir, Branch: "main"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "currently in") {
		t.Fatalf("want current-worktree guard error, got %v", err)
	}
}

func TestRemoveWorktreeGuardsPrimaryWorktree(t *testing.T) {
	dir, _ := newRepo(t)
	wt := addWorktree(t, dir, "feature/fromhere", "wt-from")
	linked := &git.Repo{Runner: gitexec.NewExecRunner("git", wt, observ.NewRing(50))}
	_, err := RemoveWorktree{Path: dir, Branch: "main"}.Run(
		context.Background(),
		OpDeps{Repo: linked, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "main worktree") {
		t.Fatalf("want primary-worktree guard error, got %v", err)
	}
}
