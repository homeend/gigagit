package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// The cached listing answers the first GET while the live listing is still
// out: a restarted page draws its PRs at once.
func TestPRsServesTheCachedListing(t *testing.T) {
	t.Parallel()
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	f := &fakeForge{open: []model.PullRequest{openPR(3, "live")}, gate: gate}
	ts, srv := prServe(t, newRepoDir(t, 1), f)
	srv.service().SeedPRListing("fake", []model.PullRequest{openPR(3, "cached")})
	var out struct {
		Available bool `json:"available"`
		Cached    bool `json:"cached"`
		PRs       []struct {
			Title string `json:"title"`
		} `json:"prs"`
	}
	if code := getJSON(t, ts, "/api/pr", &out); code != 200 {
		t.Fatalf("GET /api/pr = %d", code)
	}
	if !out.Available || !out.Cached || len(out.PRs) != 1 || out.PRs[0].Title != "cached" {
		t.Fatalf("first answer = %+v", out)
	}
	// A row the page shows from the cache opens like any other (here: not
	// fetched yet), never "unknown pull request".
	var open struct {
		State string `json:"state"`
	}
	if code := getJSON(t, ts, "/api/pr/open?n=3", &open); code != 200 || open.State != "unfetched" {
		t.Fatalf("open of a cached row = %d %+v", code, open)
	}
}

// snapForge answers a revalidation in one Snapshot call.
type snapForge struct{ *fakeForge }

func (f snapForge) Snapshot(_ context.Context, n int) (forge.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.open {
		if p.Number == n {
			return forge.Snapshot{PR: p, Comments: slices.Clone(f.comments)}, nil
		}
	}
	return forge.Snapshot{}, forge.ErrNotFound
}

// Serial: prFixture isolates XDG state with t.Setenv.
func TestPRRevalidateReportsCommentChanges(t *testing.T) {
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA = head
	f := &fakeForge{open: []model.PullRequest{pr}, baseURL: bare,
		comments: []model.ForgeComment{{ID: "c1", Kind: model.ForgeCommentInline, Path: "a", Line: 1}}}
	ts, _ := prServe(t, dir, snapForge{f})
	waitPRsLoaded(t, ts)
	var out struct {
		CommentsChanged bool   `json:"comments_changed"`
		ReadAt          string `json:"read_at"`
	}
	if code := postJSON(t, ts, "/api/pr/revalidate?n=7", "{}", "application/json", "", &out); code != 200 || !out.CommentsChanged || out.ReadAt == "" {
		t.Fatalf("first revalidate = %d %+v", code, out)
	}
	out.CommentsChanged = true
	postJSON(t, ts, "/api/pr/revalidate?n=7", "{}", "application/json", "", &out)
	if out.CommentsChanged {
		t.Fatal("unchanged comments reported as changed")
	}
	// The page's comment poll is the same one read.
	var poll struct {
		Changed bool `json:"changed"`
	}
	if code := postJSON(t, ts, "/api/pr/comments/refresh?n=7", "{}", "application/json", "", &poll); code != 200 || poll.Changed {
		t.Fatalf("comment poll = %d %+v", code, poll)
	}
	f.mu.Lock()
	calls := f.commentCalls + f.prReads
	f.mu.Unlock()
	if calls != 0 {
		t.Fatalf("PR()/Comments() called %d times beside Snapshot", calls)
	}
}

// A second page session opens a fetched PR from the cached derived data.
// Serial: prFixture isolates XDG state with t.Setenv.
func TestPROpenServesASecondSessionFromTheCache(t *testing.T) {
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA = head
	ts, _ := prServe(t, dir, &fakeForge{open: []model.PullRequest{pr}, baseURL: bare})
	waitPRsLoaded(t, ts)
	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch: %v", done)
	}
	type openResp struct {
		prOpenResp
		Cached bool `json:"cached"`
	}
	var first openResp
	if code := getJSON(t, ts, "/api/pr/open?n=7", &first); code != 200 || first.State != "ok" || first.Cached {
		t.Fatalf("first open = %d %+v", code, first)
	}
	ts2, _ := prServe(t, dir, &fakeForge{open: []model.PullRequest{pr}, baseURL: bare})
	waitPRsLoaded(t, ts2)
	var second openResp
	if code := getJSON(t, ts2, "/api/pr/open?n=7", &second); code != 200 || !second.Cached {
		t.Fatalf("second open = %d %+v", code, second)
	}
	if second.Left != first.Left || second.Right != first.Right {
		t.Fatalf("cached pair %s..%s != %s..%s", second.Left, second.Right, first.Left, first.Right)
	}
}

// Review finding 6: [forge] cache_hours = 0 holds from the very first request
// — the cached listing must not let the first listing skip detection.
func TestWebCacheHoursZeroHoldsOnTheFirstListing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte("[forge]\ncache_hours = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeForge{open: []model.PullRequest{openPR(3, "live")}}
	ts, srv := prServe(t, dir, f)
	srv.service().SeedPRListing("fake", []model.PullRequest{openPR(3, "cached")}) // fresh only under the 8 h default
	out := waitPRsLoaded(t, ts)
	if len(out.PRs) != 1 || out.PRs[0].Title != "live" {
		t.Fatalf("cache_hours = 0 served %+v", out.PRs)
	}
	f.mu.Lock()
	d := f.detects
	f.mu.Unlock()
	if d != 1 {
		t.Fatalf("Detect calls = %d, want 1 (cache_hours = 0 trusts no cached verdict)", d)
	}
}

// Item 4: opening an already-fetched PR from the page is an open — it stamps
// the cache entry's open time (the disk cache keeps the most recently opened
// PRs; the heartbeat's revalidate no longer stamps). Serial: prFixture.
func TestPROpenStampsTheOpenTime(t *testing.T) {
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA = head
	ts, _ := prServe(t, dir, &fakeForge{open: []model.PullRequest{pr}, baseURL: bare})
	waitPRsLoaded(t, ts)
	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch: %v", done)
	}
	opened := func() time.Time {
		t.Helper()
		m, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_STATE_HOME"), "gg", "prcache", "*", "pr-7.json"))
		if len(m) != 1 {
			t.Fatalf("entries %v", m)
		}
		b, err := os.ReadFile(m[0])
		if err != nil {
			t.Fatal(err)
		}
		var e struct {
			OpenedAt time.Time `json:"opened_at"`
		}
		if err := json.Unmarshal(b, &e); err != nil {
			t.Fatal(err)
		}
		return e.OpenedAt
	}
	before := opened()
	time.Sleep(20 * time.Millisecond)
	if code := getJSON(t, ts, "/api/pr/open?n=7", nil); code != 200 {
		t.Fatalf("open = %d", code)
	}
	if after := opened(); !after.After(before) {
		t.Fatalf("an open did not stamp: %v → %v", before, after)
	}
}
