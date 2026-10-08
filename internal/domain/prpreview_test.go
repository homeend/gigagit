package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/prcache"
)

// prPreviewRepo: main with a seed commit, a PR branch with two commits that
// both touch a.go, refs/gg/pr/7 at its tip. Returns the dir and that tip.
func prPreviewRepo(t *testing.T) (string, string) {
	t.Helper()
	dir, _ := newRealRepo(t)
	runGitIn(t, dir, "checkout", "-q", "-b", "feat")
	commitFile(t, dir, "a.go", "package a\n", "c1")
	commitFile(t, dir, "a.go", "package a\n\nvar X = 1\n", "c2")
	head := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "update-ref", git.PRRef(7), head)
	runGitIn(t, dir, "checkout", "-q", "-")
	return dir, head
}

// commitOnMain moves main without touching the PR's files.
func commitOnMain(t *testing.T, dir, name string) {
	t.Helper()
	commitFile(t, dir, name, "z\n", "main moves")
}

// forcePushPR points refs/gg/pr/n at a new commit off main touching b.go only.
func forcePushPR(t *testing.T, dir string, n int) string {
	t.Helper()
	runGitIn(t, dir, "checkout", "-q", "-b", "feat2")
	commitFile(t, dir, "b.go", "package b\n", "rewrite")
	head := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "update-ref", git.PRRef(n), head)
	runGitIn(t, dir, "checkout", "-q", "-")
	return head
}

// countingService is a fresh Service (empty memory caches — a restart) over
// dir, counting its git calls by name.
func countingService(t *testing.T, dir string) (*Service, func(string) int) {
	t.Helper()
	c := newCountingRunner(gitexec.NewExecRunner("git", dir, observ.NewRing(50)))
	return New(&git.Repo{Runner: c}), c.count
}

func TestPRPreviewServesASecondSessionFromTheCache(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t)
	cache := t.TempDir()
	p := model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: head}

	a, _ := countingService(t, dir)
	a.SetPRCacheStore(prcache.New(cache, 0))
	first, err := a.PRPreview(context.Background(), p)
	if err != nil || first.Endpoints.Summary.State != PreviewOK || len(first.Files) != 1 || first.Cached {
		t.Fatalf("first open = %+v, %v", first, err)
	}

	b, calls := countingService(t, dir) // a restart: empty memory caches
	b.SetPRCacheStore(prcache.New(cache, 0))
	second, err := b.PRPreview(context.Background(), p)
	if err != nil || !second.Cached || len(second.Files) != 1 || len(second.Set.Commits) != 2 {
		t.Fatalf("second open = %+v, %v", second, err)
	}
	if second.Endpoints.Summary.Ahead != 2 || second.Endpoints.Summary.Files != 1 || second.Set.Base == "" {
		t.Fatalf("second summary = %+v, set base %q", second.Endpoints.Summary, second.Set.Base)
	}
	// The merge base and the ahead count are computed live (cheap, and they
	// are the key); the slow three come from the cache.
	for _, name := range []string{"git diff --name-only (range)", "git rev-list (range)", "git diff (compare files)"} {
		if calls(name) != 0 {
			t.Errorf("%s ran %d times on a cached open, want 0", name, calls(name))
		}
	}
	// The compare view's own reads hit the memory caches PRPreview seeded.
	if _, err := b.CompareFiles(context.Background(), second.Endpoints.Left, second.Endpoints.Right); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PreviewNotes(context.Background(), second.Pair.Head, second.Pair.Base); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"git diff --name-only (range)", "git rev-list (range)", "git diff (compare files)"} {
		if calls(name) != 0 {
			t.Errorf("%s ran %d times after PRPreview seeded it", name, calls(name))
		}
	}
}

// The base branch moving WITHOUT a new merge base is still a cache hit.
func TestPRPreviewHitsAfterTheBaseBranchMoved(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t)
	cache := t.TempDir()
	p := model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: head}
	a, _ := countingService(t, dir)
	a.SetPRCacheStore(prcache.New(cache, 0))
	if _, err := a.PRPreview(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	commitOnMain(t, dir, "z.txt")
	b, _ := countingService(t, dir)
	b.SetPRCacheStore(prcache.New(cache, 0))
	got, err := b.PRPreview(context.Background(), p)
	if err != nil || !got.Cached || len(got.Files) != 1 {
		t.Fatalf("after main moved: %+v, %v", got, err)
	}
}

// Review focus 3: a force-push is a new pair; the old pair's data is never served.
func TestPRPreviewRecomputesForANewHead(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t)
	cache := t.TempDir()
	svc, _ := countingService(t, dir)
	svc.SetPRCacheStore(prcache.New(cache, 0))
	if _, err := svc.PRPreview(context.Background(), model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: head}); err != nil {
		t.Fatal(err)
	}
	newHead := forcePushPR(t, dir, 7)
	svc2, _ := countingService(t, dir)
	svc2.SetPRCacheStore(prcache.New(cache, 0))
	got, err := svc2.PRPreview(context.Background(), model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: newHead})
	if err != nil || got.Cached {
		t.Fatalf("new head served from cache: %+v, %v", got, err)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "b.go" {
		t.Fatalf("files for the new head = %+v", got.Files)
	}
}

// A merged PR (nothing ahead) and a missing head report their states.
func TestPRPreviewStates(t *testing.T) {
	t.Parallel()
	dir, _ := prPreviewRepo(t)
	svc, _ := countingService(t, dir)
	svc.SetPRCacheStore(prcache.New(t.TempDir(), 0))
	got, err := svc.PRPreview(context.Background(), model.PullRequest{Number: 9, State: "open", Target: "main"})
	if err != nil || got.Endpoints.Summary.State != PreviewMissingSource {
		t.Fatalf("missing head = %+v, %v", got.Endpoints.Summary, err)
	}
	runGitIn(t, dir, "update-ref", git.PRRef(8), revParse(t, dir, "main"))
	got, err = svc.PRPreview(context.Background(), model.PullRequest{Number: 8, State: "open", Target: "main"})
	if err != nil || got.Endpoints.Summary.State != PreviewMerged {
		t.Fatalf("merged = %+v, %v", got.Endpoints.Summary, err)
	}
}

// Item 7: the commits PR n's head has that the head on screen had not; a
// force-push counts every commit not in the old head; a non-sha is 0.
func TestPRNewCommits(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t)
	svc, _ := countingService(t, dir)
	ctx := context.Background()
	if n := svc.PRNewCommits(ctx, 7, revParse(t, dir, head+"~1")); n != 1 {
		t.Fatalf("one commit on top = %d", n)
	}
	if n := svc.PRNewCommits(ctx, 7, "HEAD"); n != 0 {
		t.Fatalf("a non-sha counted %d", n)
	}
	forcePushPR(t, dir, 7)
	if n := svc.PRNewCommits(ctx, 7, head); n != 1 {
		t.Fatalf("after a force-push = %d, want the 1 commit not in the old head", n)
	}
}
