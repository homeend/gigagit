package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// rewrittenRepo builds the shape a rejected push after a LOCAL rebase leaves
// behind: `old` stands for the remote tip (the branch as it was published),
// HEAD is the same two commits replayed onto an advanced main.
func rewrittenRepo(t *testing.T) (string, *Repo) {
	t.Helper()
	dir, _ := newTestRepo(t)
	writeFile(t, dir, "f1.txt", "one\n")
	commitAll(t, dir, "F1")
	writeFile(t, dir, "f2.txt", "two\n")
	commitAll(t, dir, "F2")
	gitRun(t, dir, "branch", "old") // the published copy
	gitRun(t, dir, "checkout", "main~2")
	gitRun(t, dir, "checkout", "-b", "trunk")
	writeFile(t, dir, "m1.txt", "trunk\n")
	commitAll(t, dir, "M1")
	gitRun(t, dir, "checkout", "main")
	gitRun(t, dir, "rebase", "trunk")
	return dir, &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

func TestCountRangeCountsCommitsMissingFromHead(t *testing.T) {
	t.Parallel()
	_, repo := rewrittenRepo(t)
	n, err := repo.CountRange(context.Background(), "HEAD", "old")
	if err != nil {
		t.Fatalf("CountRange: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountRange(HEAD, old) = %d, want 2 (both published copies)", n)
	}
}

func TestCountRangeUniqueIgnoresPatchEquivalentRewrites(t *testing.T) {
	t.Parallel()
	_, repo := rewrittenRepo(t)
	n, err := repo.CountRangeUnique(context.Background(), "HEAD", "old")
	if err != nil {
		t.Fatalf("CountRangeUnique: %v", err)
	}
	if n != 0 {
		t.Fatalf("CountRangeUnique(HEAD, old) = %d, want 0 — every published commit is a patch copy of ours", n)
	}
}

func TestCountRangeUniqueCountsGenuinelyNewCommits(t *testing.T) {
	t.Parallel()
	dir, _ := newTestRepo(t)
	gitRun(t, dir, "branch", "old")
	writeFile(t, dir, "mine.txt", "mine\n")
	commitAll(t, dir, "F1")
	gitRun(t, dir, "checkout", "old")
	writeFile(t, dir, "theirs.txt", "theirs\n")
	commitAll(t, dir, "N1")
	gitRun(t, dir, "checkout", "main")
	repo := &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}

	total, err := repo.CountRange(context.Background(), "HEAD", "old")
	if err != nil {
		t.Fatalf("CountRange: %v", err)
	}
	uniq, err := repo.CountRangeUnique(context.Background(), "HEAD", "old")
	if err != nil {
		t.Fatalf("CountRangeUnique: %v", err)
	}
	if total != 1 || uniq != 1 {
		t.Fatalf("total=%d unique=%d, want 1 and 1 — their commit is real new work", total, uniq)
	}
}

func TestRemoteBranchTipResolvesWithoutFetching(t *testing.T) {
	t.Parallel()
	dir, _ := newTestRepo(t)
	origin := t.TempDir()
	gitRun(t, origin, "init", "--bare", ".")
	gitRun(t, dir, "remote", "add", "origin", origin)
	gitRun(t, dir, "push", "origin", "main")
	repo := &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}

	tip, err := repo.RemoteBranchTip(context.Background(), "origin", "main")
	if err != nil {
		t.Fatalf("RemoteBranchTip: %v", err)
	}
	want := gitOutIn(t, dir, "rev-parse", "main")
	if tip == "" || tip != want {
		t.Fatalf("RemoteBranchTip = %q, want %q", tip, want)
	}
}

func TestRemoteBranchTipEmptyForAbsentBranch(t *testing.T) {
	t.Parallel()
	dir, _ := newTestRepo(t)
	origin := t.TempDir()
	gitRun(t, origin, "init", "--bare", ".")
	gitRun(t, dir, "remote", "add", "origin", origin)
	gitRun(t, dir, "push", "origin", "main")
	repo := &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}

	tip, err := repo.RemoteBranchTip(context.Background(), "origin", "nope")
	if err != nil {
		t.Fatalf("RemoteBranchTip(absent): %v", err)
	}
	if tip != "" {
		t.Fatalf("RemoteBranchTip(absent) = %q, want \"\"", tip)
	}
}

func TestBranchReflogContainsFindsPreRebaseTip(t *testing.T) {
	t.Parallel()
	_, repo := rewrittenRepo(t)
	old := repo.mustRev(t, "old")
	ok, err := repo.BranchReflogContains(context.Background(), "main", old)
	if err != nil {
		t.Fatalf("BranchReflogContains: %v", err)
	}
	if !ok {
		t.Fatal("BranchReflogContains(main, pre-rebase tip) = false, want true — main pointed there before the rebase")
	}
}

func TestBranchReflogContainsRejectsForeignCommit(t *testing.T) {
	t.Parallel()
	_, repo := rewrittenRepo(t)
	trunk := repo.mustRev(t, "trunk")
	ok, err := repo.BranchReflogContains(context.Background(), "main", trunk)
	if err != nil {
		t.Fatalf("BranchReflogContains: %v", err)
	}
	if ok {
		t.Fatal("BranchReflogContains(main, trunk tip) = true, want false — main never pointed at trunk's commit")
	}
}

func TestBranchReflogContainsMissingReflogIsNotAnError(t *testing.T) {
	t.Parallel()
	dir, repo := rewrittenRepo(t)
	old := repo.mustRev(t, "old")
	// Simulate an expired/absent reflog for the branch under test.
	if err := os.Remove(filepath.Join(dir, ".git", "logs", "refs", "heads", "main")); err != nil {
		t.Fatalf("remove reflog: %v", err)
	}
	ok, err := repo.BranchReflogContains(context.Background(), "main", old)
	if err != nil {
		t.Fatalf("BranchReflogContains(no reflog): %v, want nil — absence is no evidence, not a failure", err)
	}
	if ok {
		t.Fatal("BranchReflogContains(no reflog) = true, want false")
	}
}

// mustRev resolves a ref through the test repo or fails the test.
func (r *Repo) mustRev(t *testing.T, ref string) string {
	t.Helper()
	res, err := r.Runner.Run(context.Background(), "git rev-parse", []string{"rev-parse", ref})
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(res.Stdout)
}
