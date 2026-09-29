package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remoteRecycleFixture: recycleFixture plus a genuine remote-tracking branch
// origin/foo (one commit ahead of main) with no local foo.
func remoteRecycleFixture(t *testing.T) (string, OpDeps, string) {
	t.Helper()
	dir, deps, wt := recycleFixture(t)
	configRemote(t, dir)
	gitIn(t, dir, "commit", "--allow-empty", "-m", "remote work")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/foo", "HEAD")
	gitIn(t, dir, "reset", "--hard", "HEAD~1")
	return dir, deps, wt
}

func localExists(t *testing.T, deps OpDeps, name string) bool {
	t.Helper()
	ok, err := deps.Repo.LocalBranchExists(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestRecycleRemoteCreatesTrackingBranch(t *testing.T) {
	t.Parallel()
	dir, deps, wt := remoteRecycleFixture(t)
	res, err := RecycleWorktree{Dir: wt, Branch: "foo", RemoteRef: "origin/foo"}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasPrefix(res.Summary, "recycled a → foo in ") || !strings.Contains(res.Summary, "from origin/foo") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if got := wtHead(t, wt); got != "foo" {
		t.Fatalf("worktree HEAD = %q, want foo", got)
	}
	if up := gitOut(t, dir, "rev-parse", "--abbrev-ref", "foo@{upstream}"); up != "origin/foo" {
		t.Fatalf("foo upstream = %q, want origin/foo", up)
	}
}

func TestRecycleRemoteFastForwardsExistingLocal(t *testing.T) {
	t.Parallel()
	dir, deps, wt := remoteRecycleFixture(t)
	gitIn(t, dir, "branch", "foo", "main") // behind origin/foo
	if _, err := (RecycleWorktree{Dir: wt, Branch: "foo", RemoteRef: "origin/foo"}).Run(context.Background(), deps); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if a, b := gitOut(t, dir, "rev-parse", "foo"), gitOut(t, dir, "rev-parse", "origin/foo"); a != b {
		t.Fatalf("foo = %s, want origin/foo %s", a, b)
	}
	if got := wtHead(t, wt); got != "foo" {
		t.Fatalf("worktree HEAD = %q, want foo", got)
	}
}

func TestRecycleRemoteDivergedRefusesUntouched(t *testing.T) {
	t.Parallel()
	dir, deps, wt := remoteRecycleFixture(t)
	gitIn(t, dir, "branch", "foo", "main")
	gitIn(t, dir, "switch", "foo")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "local work")
	gitIn(t, dir, "switch", "main")
	_, err := RecycleWorktree{Dir: wt, Branch: "foo", RemoteRef: "origin/foo"}.Run(context.Background(), deps)
	var div CheckoutDivergedError
	if !errors.As(err, &div) {
		t.Fatalf("err = %v, want CheckoutDivergedError", err)
	}
	if got := wtHead(t, wt); got != "a" {
		t.Fatalf("worktree HEAD = %q, want a (untouched)", got)
	}
}

func TestRecycleRemoteTargetRefusalCreatesNoBranch(t *testing.T) {
	t.Parallel()
	_, deps, wt := remoteRecycleFixture(t)
	gitDir := gitOut(t, wt, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
	os.WriteFile(filepath.Join(gitDir, "MERGE_HEAD"), []byte("0000000000000000000000000000000000000000\n"), 0o644)
	_, err := RecycleWorktree{Dir: wt, Branch: "foo", RemoteRef: "origin/foo"}.Run(context.Background(), deps)
	if err == nil || !strings.Contains(err.Error(), "merge in progress") {
		t.Fatalf("err = %v", err)
	}
	if localExists(t, deps, "foo") {
		t.Fatal("a refused recycle must not create foo")
	}
}

func TestRecycleRemoteAbortKeepsCheckedOutBranch(t *testing.T) {
	t.Parallel()
	_, deps, wt := remoteRecycleFixture(t)
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("edited\n"), 0o644)
	deps.Decider = MapDecider{RecycleDirtyDecisionID: "abort"}
	res, err := RecycleWorktree{Dir: wt, Branch: "foo", RemoteRef: "origin/foo"}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !localExists(t, deps, "foo") {
		t.Fatal("the check-out ran before the prompt; foo must stay")
	}
	if !strings.Contains(res.Summary, "recycle cancelled") || !strings.Contains(res.Summary, "origin/foo") {
		t.Fatalf("summary = %q; it must say the branch was still checked out", res.Summary)
	}
	if got := wtHead(t, wt); got != "a" {
		t.Fatalf("worktree HEAD = %q, want a", got)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "README.md")); string(b) != "edited\n" {
		t.Fatalf("README = %q, the tree must be untouched", b)
	}
}
