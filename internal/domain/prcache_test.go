package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prcache"
	"github.com/homeend/gigagit/internal/repogate"
)

func newCachedForgeSvc(t *testing.T, ff *fakeForge, dir string) *Service {
	t.Helper()
	svc := newForgeSvc(t, ff)
	svc.SetPRCacheStore(prcache.New(dir, prcache.DefaultMax))
	return svc
}

func atClock(svc *Service, now *time.Time) { svc.forgeNow = func() time.Time { return *now } }

// A PR read in one session opens in the next with no forge call, until the
// read is older than the limit.
func TestPRCacheSurvivesANewSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)}}
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	a := newCachedForgeSvc(t, ff, dir)
	atClock(a, &now)
	if _, err := a.PRFetchOp(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	now = now.Add(7 * time.Hour)
	b := newCachedForgeSvc(t, ff, dir) // a restart
	atClock(b, &now)
	if _, err := b.PRFetchOp(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if n, _ := ff.calls(7); n != 1 {
		t.Fatalf("PR() called %d times across two sessions within 8h, want 1", n)
	}
	now = now.Add(2 * time.Hour) // 9h after the read
	c := newCachedForgeSvc(t, ff, dir)
	atClock(c, &now)
	if _, err := c.PRFetchOp(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if n, _ := ff.calls(7); n != 2 {
		t.Fatalf("an expired entry must be read again: PR() called %d times, want 2", n)
	}
}

// Review focus 4: cache_hours = 0 never serves a cached entry.
func TestPRCacheZeroHoursAlwaysReads(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)}, open: []model.PullRequest{pr(7, "open", 1)}}
	svc := newCachedForgeSvc(t, ff, t.TempDir())
	svc.SetPRCachePolicy(0, 0)
	for range 2 {
		if _, err := svc.PRFetchOp(context.Background(), 7); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := ff.calls(7); n != 2 {
		t.Fatalf("cache_hours=0: PR() called %d times, want 2", n)
	}
	if _, err := svc.PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.PullRequestsCached(); ok {
		t.Fatal("cache_hours=0: no cached listing may be served")
	}
}

// Comments read in one session are drawn in the next before any forge call.
func TestPRCommentsServedFromDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)},
		comments: []model.ForgeComment{{ID: "c1", Kind: model.ForgeCommentInline, Path: "a.go", Line: 2, Body: "x"}}}
	a := newCachedForgeSvc(t, ff, dir)
	if _, err := a.PRCommentsRefresh(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	b := newCachedForgeSvc(t, ff, dir)
	c, ok := b.PRCommentsCached(7)
	if !ok || len(c.Inline) != 1 || c.Inline[0].ID != "c1" {
		t.Fatalf("PRCommentsCached in a new session = %+v, %v", c, ok)
	}
}

// The listing is cached; a new session draws it with no forge call.
func TestPullRequestsCached(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	a := newCachedForgeSvc(t, ff, dir)
	if _, err := a.PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	b := newCachedForgeSvc(t, ff, dir)
	got, ok := b.PullRequestsCached()
	if !ok || len(got) != 1 || got[0].Number != 3 {
		t.Fatalf("PullRequestsCached = %+v, %v", got, ok)
	}
}

// Opening a PR the cached LIST shows costs no forge call in a new session,
// and the base repository is asked once across sessions.
func TestPROpenFromTheCachedListCostsNoForgeCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{slug: "github.com/o/r", url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	a := newCachedForgeSvc(t, ff, dir)
	if _, err := a.PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PRFetchOp(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	_, baseA := ff.calls(3)
	b := newCachedForgeSvc(t, ff, dir)
	if _, err := b.PRFetchOp(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	if n, base := ff.calls(3); n != 0 || base != baseA {
		t.Fatalf("second session: PR() %d (want 0), BaseRepo %d (want %d)", n, base, baseA)
	}
}

// A listing row never wipes what a full read learned (body, node id, viewer).
func TestListPollKeepsFullReadFields(t *testing.T) {
	t.Parallel()
	full := pr(7, "open", 1)
	full.Body, full.NodeID, full.ViewerDidAuthor, full.ViewerPendingReview = "b", "PR_x", true, "PRR_p"
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: full}, open: []model.PullRequest{pr(7, "open", 1)}}
	svc := newCachedForgeSvc(t, ff, t.TempDir())
	if _, err := svc.PullRequest(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _, ok := svc.PRDetailsCached(7)
	_ = ok
	svc.forgeMu.Lock()
	e := svc.forgePRCache[7]
	svc.forgeMu.Unlock()
	if e.pr.NodeID != "PR_x" || e.pr.Body != "b" || !e.pr.ViewerDidAuthor || e.pr.ViewerPendingReview != "PRR_p" {
		t.Fatalf("after a list poll: %+v (details %+v)", e.pr, got)
	}
}

// Forget drops the disk entry too.
func TestPRForgetRemovesTheDiskEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "closed", 1)}}
	svc := newCachedForgeSvc(t, ff, dir)
	if _, err := svc.PRFetchOp(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if _, ok := prcache.New(dir, 0).Load(7); !ok {
		t.Fatal("PRFetchOp did not persist the entry")
	}
	svc.PRForgetOp(7)
	if _, ok := prcache.New(dir, 0).Load(7); ok {
		t.Fatal("PRForgetOp left the disk entry")
	}
}

// snapFake answers a revalidation in one Snapshot call.
type snapFake struct {
	*fakeForge
	snaps int
}

func (f *snapFake) Snapshot(_ context.Context, n int) (forge.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snaps++
	return forge.Snapshot{PR: f.byNum[n], Comments: append([]model.ForgeComment(nil), f.comments...)}, nil
}

func TestPRRevalidateIsOneCallAndReportsComments(t *testing.T) {
	t.Parallel()
	ff := &snapFake{fakeForge: &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)},
		comments: []model.ForgeComment{{ID: "c1", Kind: model.ForgeCommentInline, Path: "a", Line: 1}}}}
	_, svc := newRealRepo(t)
	svc.SetForgeProviders([]forge.Provider{ff})
	svc.SetPRCacheStore(prcache.New(t.TempDir(), 0))
	rv, err := svc.PRRevalidate(context.Background(), 7)
	if err != nil || !rv.CommentsChanged || rv.ReadAt.IsZero() {
		t.Fatalf("first revalidate = %+v, %v", rv, err)
	}
	rv, _ = svc.PRRevalidate(context.Background(), 7)
	if rv.CommentsChanged {
		t.Fatal("unchanged comments reported as changed")
	}
	ff.mu.Lock()
	ff.comments = append(ff.comments, model.ForgeComment{ID: "c2", Kind: model.ForgeCommentInline, Path: "a", Line: 2})
	ff.mu.Unlock()
	if rv, _ = svc.PRRevalidate(context.Background(), 7); !rv.CommentsChanged {
		t.Fatal("a new comment not reported")
	}
	if ff.snaps != 3 {
		t.Fatalf("Snapshot calls = %d, want 3", ff.snaps)
	}
	if n, _ := ff.calls(7); n != 0 || ff.commentCalls != 0 {
		t.Fatalf("PR() %d / Comments() %d beside Snapshot, want 0/0", n, ff.commentCalls)
	}
	if c, ok := svc.PRCommentsCached(7); !ok || len(c.Inline) != 2 {
		t.Fatalf("revalidate did not fill the comment cache: %+v %v", c, ok)
	}
}

// A provider without Snapshot still revalidates (two calls) and reports
// comment changes.
func TestPRRevalidateWithoutSnapshot(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{7: pr(7, "open", 1)},
		comments: []model.ForgeComment{{ID: "c1", Kind: model.ForgeCommentInline, Path: "a", Line: 1}}}
	svc := newCachedForgeSvc(t, ff, t.TempDir())
	rv, err := svc.PRRevalidate(context.Background(), 7)
	if err != nil || !rv.CommentsChanged {
		t.Fatalf("revalidate = %+v, %v", rv, err)
	}
	if n, _ := ff.calls(7); n != 1 || ff.commentCalls != 1 {
		t.Fatalf("PR() %d / Comments() %d, want 1/1", n, ff.commentCalls)
	}
}

// After a good session, the next one lists without a Detect call — in the
// frontends' real order: ForgeStatus first, then PullRequests.
func TestPullRequestsSkipsDetectAfterAGoodSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	a := newCachedForgeSvc(t, ff, dir)
	a.ForgeStatus(context.Background())
	if _, err := a.PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := ff.detects.Load()
	b := newCachedForgeSvc(t, ff, dir)
	if st := b.ForgeStatus(context.Background()); !st.Available() || st.Provider != "fake" {
		t.Fatalf("the cached verdict must make the forge available: %+v", st)
	}
	if _, err := b.PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := ff.detects.Load() - before; n != 0 {
		t.Fatalf("Detect ran %d more times", n)
	}
}

// A failing optimistic list undoes the verdict and falls back to detection.
func TestPullRequestsFallsBackToDetect(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ok := &fakeForge{url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	if _, err := newCachedForgeSvc(t, ok, dir).PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	broken := &fakeForge{url: "u", listErr: errors.New("401"), detectErr: errors.New("logged out")}
	b := newCachedForgeSvc(t, broken, dir)
	if st := b.ForgeStatus(context.Background()); !st.Available() {
		t.Fatal("optimistic verdict expected before the list")
	}
	if _, err := b.PullRequests(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	if n := broken.detects.Load(); n != 1 {
		t.Fatalf("Detect calls = %d, want 1", n)
	}
	if st := b.ForgeStatus(context.Background()); st.Available() {
		t.Fatal("after the real detection failed the forge must be unavailable")
	}
}

// refspecFake fetches PR heads from a local branch (offline prefetch tests).
type refspecFake struct {
	*fakeForge
	spec string
}

func (f refspecFake) HeadRefspec(int) string { return f.spec }

// Review focus 4: prefetch = 0 does nothing; an unmoved head is skipped; a
// user op holding the gate makes prefetch step aside; a moved head is warmed.
func TestPRPrefetch(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t)
	svc, _ := countingService(t, dir)
	svc.SetPRCacheStore(prcache.New(t.TempDir(), 0))
	ff := &fakeForge{url: dir, open: []model.PullRequest{{Number: 7, State: "open", Target: "main", HeadSHA: head}}}
	svc.SetForgeProviders([]forge.Provider{refspecFake{ff, "refs/heads/feat"}})
	ctx := context.Background()
	if _, err := svc.PullRequests(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PRPreview(ctx, ff.open[0]); err != nil { // opened once → an entry
		t.Fatal(err)
	}
	svc.SetPRCachePolicy(8*time.Hour, 0)
	if n := svc.PRPrefetch(ctx); n != 0 {
		t.Fatalf("prefetch=0 warmed %d", n)
	}
	svc.SetPRCachePolicy(8*time.Hour, 5)
	if n := svc.PRPrefetch(ctx); n != 0 {
		t.Fatalf("an unmoved head was prefetched (%d)", n)
	}
	// The forge's head moves past refs/gg/pr/7.
	runGitIn(t, dir, "checkout", "-q", "feat")
	commitFile(t, dir, "c.go", "package c\n", "c3")
	moved := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "checkout", "-q", "-")
	ff.setOpen(model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: moved})
	if _, err := svc.PullRequests(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := svc.gateFor(ctx).Acquire(ctx, repogate.TreeWrite, "user op")
	if err != nil {
		t.Fatal(err)
	}
	if n := svc.PRPrefetch(ctx); n != 0 {
		t.Fatalf("prefetch ran under a user op (%d)", n)
	}
	res.Release()
	if n := svc.PRPrefetch(ctx); n != 1 {
		t.Fatalf("prefetch after the op = %d, want 1", n)
	}
	if got := revParse(t, dir, git.PRRef(7)); got != moved {
		t.Fatalf("refs/gg/pr/7 = %s, want the moved head %s", got, moved)
	}
}
