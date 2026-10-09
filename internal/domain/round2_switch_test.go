package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A sibling checkout whose directory name EXTENDS the checkout's is not
// inside it: the prefix test runs on the key with its own separator, never
// on a cleaned key that lost the trailing slash.
func TestLinkSplitSiblingCheckoutIsOutside(t *testing.T) {
	t.Parallel()
	if rel, ok := linkSplit("/w/fast-worktree-switch-2/x.go", "/w/fast-worktree-switch"); ok {
		t.Fatalf("a sibling checkout resolved inside: rel=%q", rel)
	}
	if rel, ok := linkSplit("/w/fast-worktree-switch/a/b.go", "/w/fast-worktree-switch"); !ok || rel != "a/b.go" {
		t.Fatalf("a file inside: rel=%q ok=%v", rel, ok)
	}
	if rel, ok := linkSplit("/w/fast-worktree-switch", "/w/fast-worktree-switch/"); !ok || rel != "" {
		t.Fatalf("the checkout itself: rel=%q ok=%v", rel, ok)
	}
}

// A feed re-rooted at another worktree walks from THAT tree's HEAD: a
// commit only its detached HEAD reaches shows after the next refresh.
func TestCommitFeedSetServiceWalksTheNewRoot(t *testing.T) {
	t.Parallel()
	dir, home := newRealRepo(t)
	other := filepath.Join(t.TempDir(), "wt2")
	runGitIn(t, dir, "worktree", "add", "--detach", other)
	if err := os.WriteFile(filepath.Join(other, "loose.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, other, "add", "loose.txt")
	runGitIn(t, other, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "loose")
	loose := headOf(t, other)
	_, svcB := newRealRepoAt(t, other)

	feed := home.CommitFeed()
	if _, err := feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	feed.SetService(svcB)
	st, err := feed.Refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range st.Commits {
		if c.Hash == loose {
			return
		}
	}
	t.Fatalf("the detached HEAD's commit %s is missing: the feed still walks the old root", loose)
}

// Re-rooting while a walk is in flight: the walk reads its pager once, under
// the lock (a data race otherwise, which -race reports), and the page it
// brings back is dropped — it was walked from the old root.
func TestCommitFeedSetServiceRacesAWalk(t *testing.T) {
	t.Parallel()
	dir, home := newRealRepo(t)
	_, again := newRealRepoAt(t, dir)
	feed := home.CommitFeed()
	if _, err := feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10 {
			_, _ = feed.Refresh(context.Background())
		}
	}()
	for i := 0; ; i++ { // re-root for as long as the walks run, so the two overlap
		select {
		case <-done:
			return
		default:
		}
		if i%2 == 0 {
			feed.SetService(again)
		} else {
			feed.SetService(home)
		}
		runtime.Gosched()
	}
}

// A page started before the re-root does not land: the generation moved.
func TestCommitFeedSetServiceSupersedesAWalk(t *testing.T) {
	t.Parallel()
	dir, home := newRealRepo(t)
	_, again := newRealRepoAt(t, dir)
	feed := home.CommitFeed()
	if _, err := feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	gen := feed.Gen()
	feed.SetService(again)
	if feed.Gen() == gen {
		t.Fatal("the generation did not move: a page walked from the old root would still land")
	}
}

// Under a case-insensitive fold a key may be LONGER than its path (a
// lower-cased İ is three bytes): the remainder is sliced off the path by
// the path's own length, never the key's. Sequential: it sets the fold.
func TestLinkSplitRemainderIsSlicedByThePath(t *testing.T) {
	was := model.CaseInsensitivePaths()
	model.SetCaseInsensitivePaths(true)
	t.Cleanup(func() { model.SetCaseInsensitivePaths(was) })
	rel, ok := linkSplit("/w/İx/a.go", "/w/İx")
	if !ok || rel != "a.go" {
		t.Fatalf("rel=%q ok=%v, want a.go", rel, ok)
	}
}
