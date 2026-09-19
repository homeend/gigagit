package web

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// fakeForge is an in-process forge.Provider: no gh, no env, so these tests
// run parallel. (A _test.go file may import internal/forge; archtest guards
// the package's non-test imports.)
type fakeForge struct {
	mu      sync.Mutex
	detect  error
	listErr error
	open    []model.PullRequest
	byN     map[int]model.PullRequest
	lists   int
	baseURL string
	gate    chan struct{} // when non-nil, ListOpen blocks until it is closed
}

func (f *fakeForge) Name() string { return "fake" }
func (f *fakeForge) Detect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.detect
}
func (f *fakeForge) ListOpen(ctx context.Context) ([]model.PullRequest, error) {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	return slices.Clone(f.open), f.listErr
}
func (f *fakeForge) PR(_ context.Context, n int) (model.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.open {
		if p.Number == n {
			return p, nil
		}
	}
	if p, ok := f.byN[n]; ok {
		return p, nil
	}
	return model.PullRequest{}, forge.ErrNotFound
}
func (f *fakeForge) Comments(context.Context, int) ([]model.ForgeComment, bool, error) {
	return nil, false, nil
}
func (f *fakeForge) BaseRepo(context.Context) (string, string, error) { return "", f.baseURL, nil }
func (f *fakeForge) HeadRefspec(n int) string                         { return fmt.Sprintf("refs/pull/%d/head", n) }

func (f *fakeForge) listCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

func openPR(n int, title string) model.PullRequest {
	return model.PullRequest{
		Number: n, Title: title, Author: "ann", State: model.PRStateOpen,
		ReviewState: "approved", Source: "feat/x", Target: "main",
		URL: fmt.Sprintf("https://example.test/pr/%d", n), Updated: time.Unix(int64(1_700_000_000+n), 0).UTC(),
	}
}

// prServe serves dir with f as the only forge provider and a ticker-less
// live hub, so a test can watch emits without the global clock seams.
func prServe(t *testing.T, dir string, f *fakeForge) (*httptest.Server, *Server) {
	t.Helper()
	svc := domain.Open(dir)
	svc.SetForgeProviders([]forge.Provider{f})
	srv := New(svc)
	srv.liveMu.Lock()
	srv.live = newLiveHub(config.RefreshConfig{}, false, nil)
	srv.liveMu.Unlock()
	t.Cleanup(srv.Close)
	return serve(t, srv), srv
}

type prListResp struct {
	Available bool   `json:"available"`
	Loaded    bool   `json:"loaded"`
	Error     string `json:"error"`
	PRs       []struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		State       string `json:"state"`
		ReviewState string `json:"review_state"`
		Updated     string `json:"updated"`
		Fetched     bool   `json:"fetched"`
	} `json:"prs"`
}

// waitPRsLoaded polls GET /api/pr until the background lane has answered.
func waitPRsLoaded(t *testing.T, ts *httptest.Server) prListResp {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var out prListResp
		if code := getJSON(t, ts, "/api/pr", &out); code != 200 {
			t.Fatalf("GET /api/pr = %d", code)
		}
		if out.Loaded {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatal("the PR list never loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPRListUnavailableStaysHidden(t *testing.T) {
	t.Parallel()
	ts, _ := prServe(t, newRepoDir(t, 1), &fakeForge{detect: errors.New("gh: not logged in")})
	out := waitPRsLoaded(t, ts)
	if out.Available || out.PRs == nil || len(out.PRs) != 0 {
		t.Fatalf("no usable forge must read as unavailable with [] rows, got %+v", out)
	}
}

func TestPRListLoadsInBackgroundAndEmits(t *testing.T) {
	t.Parallel()
	f := &fakeForge{open: []model.PullRequest{openPR(7, "Add a thing")}, gate: make(chan struct{})}
	ts, srv := prServe(t, newRepoDir(t, 1), f)
	ch, cancel := srv.liveHubRef().subscribe()
	defer cancel()

	var first prListResp
	done := make(chan struct{})
	go func() { defer close(done); getJSON(t, ts, "/api/pr", &first) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GET /api/pr blocked on the forge")
	}
	if first.Loaded || first.Available {
		t.Fatalf("first answer must be the unloaded one, got %+v", first)
	}
	close(f.gate)
	m, ok := recvLive(t, ch, 10*time.Second)
	if !ok || !slices.Equal(m.Changed, []string{"prs"}) || m.Reason != "prs" {
		t.Fatalf("want a prs emit, got %+v ok=%v", m, ok)
	}
	var out prListResp
	getJSON(t, ts, "/api/pr", &out)
	if !out.Available || !out.Loaded || len(out.PRs) != 1 {
		t.Fatalf("got %+v", out)
	}
	p := out.PRs[0]
	if p.Number != 7 || p.State != "open" || p.ReviewState != "approved" || p.Updated == "" || p.Fetched {
		t.Errorf("row = %+v", p)
	}
}

func TestPRListGetNeverCallsForge(t *testing.T) {
	t.Parallel()
	f := &fakeForge{open: []model.PullRequest{openPR(7, "t")}}
	ts, _ := prServe(t, newRepoDir(t, 1), f)
	waitPRsLoaded(t, ts)
	for range 5 {
		getJSON(t, ts, "/api/pr", nil)
	}
	if n := f.listCount(); n != 1 {
		t.Errorf("forge listed %d times, want 1 — a GET must answer from memory", n)
	}
}

func TestPRRefreshReloads(t *testing.T) {
	t.Parallel()
	f := &fakeForge{open: []model.PullRequest{openPR(7, "old title")}}
	ts, _ := prServe(t, newRepoDir(t, 1), f)
	waitPRsLoaded(t, ts)

	f.mu.Lock()
	f.open = []model.PullRequest{openPR(7, "new title")}
	f.mu.Unlock()
	var out prListResp
	if code := postJSON(t, ts, "/api/pr/refresh", "{}", "application/json", "", &out); code != 200 {
		t.Fatalf("refresh = %d", code)
	}
	if f.listCount() != 2 || len(out.PRs) != 1 || out.PRs[0].Title != "new title" {
		t.Fatalf("lists=%d out=%+v", f.listCount(), out)
	}

	f.mu.Lock()
	f.listErr = errors.New("gh: rate limited")
	f.mu.Unlock()
	out = prListResp{}
	postJSON(t, ts, "/api/pr/refresh", "{}", "application/json", "", &out)
	if out.Error == "" || len(out.PRs) != 1 || out.PRs[0].Title != "new title" {
		t.Errorf("a failed list must keep the rows and say why, got %+v", out)
	}
}

func TestPRRefreshIsWriteGuarded(t *testing.T) {
	t.Parallel()
	ts, _ := prServe(t, newRepoDir(t, 1), &fakeForge{})
	if code := postJSON(t, ts, "/api/pr/refresh", "{}", "application/json", "https://evil.example", nil); code != 403 {
		t.Errorf("cross-origin refresh = %d, want 403", code)
	}
	if code := postJSON(t, ts, "/api/pr/refresh", "{}", "text/plain", "", nil); code != 415 {
		t.Errorf("non-JSON refresh = %d, want 415", code)
	}
}

func TestPRCacheIsPerService(t *testing.T) {
	t.Parallel()
	f := &fakeForge{open: []model.PullRequest{openPR(7, "t")}}
	ts, srv := prServe(t, newRepoDir(t, 1), f)
	waitPRsLoaded(t, ts)

	other := domain.Open(newRepoDir(t, 2))
	other.SetForgeProviders([]forge.Provider{&fakeForge{detect: errors.New("none")}})
	srv.svc.Store(other) // what POST /api/reroot does
	if state, rows, _, _ := srv.prsSnapshot(other); state != "" || len(rows) != 0 {
		t.Fatalf("a new service must start unprobed, got state=%q rows=%d", state, len(rows))
	}
	if out := waitPRsLoaded(t, ts); out.Available || len(out.PRs) != 0 {
		t.Errorf("the previous repo's rows leaked: %+v", out)
	}
}
