package domain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
