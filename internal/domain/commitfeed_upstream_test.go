package domain

import (
	"context"
	"os/exec"
	"slices"
	"testing"
)

// TestCommitFeedWalksUpstreamWhenBehind proves that a commit reachable only via
// a remote-tracking ref (origin/main is ahead of local main) is included in the
// feed when Upstreams is set, and absent when it is not.
func TestCommitFeedWalksUpstreamWhenBehind(t *testing.T) {
	t.Parallel()
	// newRealRepo (compare_test.go) initialises a 1-commit main + returns dir+svc.
	dir, svc := newRealRepo(t)

	// Capture the hash of the initial commit (A) — local main is here.
	hashA := headHash(t, dir)

	// Create commit B on main (local main is now at B).
	runGit := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("commit", "--allow-empty", "-m", "c2")
	hashB := headHash(t, dir)

	// Point refs/remotes/origin/main at B, then reset local main back to A so
	// that local main is one commit BEHIND the remote-tracking ref.
	runGit("update-ref", "refs/remotes/origin/main", hashB)
	runGit("reset", "--hard", hashA)

	// --- WITH Upstreams: B must appear ---
	feed := svc.CommitFeed()
	feed.SetScope(LogScope{Branches: []string{"main"}, Upstreams: []string{"origin/main"}})
	st, err := feed.LoadInitial(context.Background())
	if err != nil {
		t.Fatalf("LoadInitial with upstreams: %v", err)
	}
	hashes := make([]string, len(st.Commits))
	for i, c := range st.Commits {
		hashes[i] = c.Hash
	}
	if !slices.Contains(hashes, hashB) {
		t.Fatalf("with Upstreams=[origin/main], expected commit B (%s) in feed; got %v", hashB, hashes)
	}

	// --- WITHOUT Upstreams: B must NOT appear ---
	feed2 := svc.CommitFeed()
	feed2.SetScope(LogScope{Branches: []string{"main"}})
	st2, err := feed2.LoadInitial(context.Background())
	if err != nil {
		t.Fatalf("LoadInitial without upstreams: %v", err)
	}
	hashes2 := make([]string, len(st2.Commits))
	for i, c := range st2.Commits {
		hashes2[i] = c.Hash
	}
	if slices.Contains(hashes2, hashB) {
		t.Fatalf("without Upstreams, commit B (%s) must NOT appear in feed; got %v", hashB, hashes2)
	}
}
