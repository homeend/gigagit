package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pauseRebaseOf leaves the worktree at wt as a paused rebase of branch
// leaves it: HEAD detached, rebase-merge/head-name naming the branch.
func pauseRebaseOf(t *testing.T, wt, branch string) {
	t.Helper()
	gitIn(t, wt, "checkout", "-q", "--detach")
	gitDir := gitOut(t, wt, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
	if err := os.MkdirAll(filepath.Join(gitDir, "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "rebase-merge", "head-name"), []byte("refs/heads/"+branch+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Moving the ref of a branch a paused rebase rewrites strands that rebase
// (its --continue fails to lock the moved ref): restore refuses, naming the
// worktree, as it does for a branch checked out there.
func TestRestoreRefusesBranchARebaseRewrites(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	ctx := context.Background()
	wt := addWorktree(t, dir, "wt", "wt-rebasing")
	tip, _ := repo.RevParse(ctx, "refs/heads/wt")
	ref := writeVersionRef(t, repo, "wt", "rebase", 1753100000, tip)
	pauseRebaseOf(t, wt, "wt")

	_, err := RestoreBranchVersion{Branch: "wt", Ref: ref}.Run(ctx, enabledDeps(repo))
	if err == nil || !strings.Contains(err.Error(), wt) {
		t.Fatalf("err = %v, want a refusal naming %s", err, wt)
	}
	if sha, _ := repo.RevParse(ctx, "refs/heads/wt"); sha != tip {
		t.Fatalf("refs/heads/wt moved to %s, want unchanged %s", sha, tip)
	}
}

// Recycling onto a branch another worktree's paused rebase holds fails
// BEFORE the dirty-tree question, so no discard/commit/shelve runs for a
// switch git would refuse.
func TestRecycleWorktreeRefusesBranchARebaseHolds(t *testing.T) {
	t.Parallel()
	dir, deps, wt := recycleFixture(t)
	other := addWorktree(t, dir, "b", "wt-b")
	pauseRebaseOf(t, other, "target")
	if err := os.WriteFile(filepath.Join(wt, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := RecycleWorktree{Dir: wt, Branch: "target"}.Run(context.Background(), deps)
	if err == nil || !strings.Contains(err.Error(), other) {
		t.Fatalf("err = %v, want a refusal naming %s", err, other)
	}
	if _, serr := os.Stat(filepath.Join(wt, "dirty.txt")); serr != nil {
		t.Fatalf("the dirty file is gone: %v", serr)
	}
}
