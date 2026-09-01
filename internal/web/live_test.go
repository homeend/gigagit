package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gitwatch"
)

func recvLive(t *testing.T, ch <-chan liveMsg, d time.Duration) (liveMsg, bool) {
	t.Helper()
	select {
	case m, ok := <-ch:
		return m, ok
	case <-time.After(d):
		return liveMsg{}, false
	}
}

func TestLiveHubEmitReachesEverySubscriber(t *testing.T) {
	t.Parallel()
	h := newLiveHub(config.RefreshConfig{Enabled: true}, true, nil)
	a, cancelA := h.subscribe()
	defer cancelA()
	b, cancelB := h.subscribe()
	defer cancelB()
	h.emit(liveMsg{Changed: []string{"branches"}, Reason: "watch"})
	for _, ch := range []<-chan liveMsg{a, b} {
		m, ok := recvLive(t, ch, time.Second)
		if !ok || !slices.Equal(m.Changed, []string{"branches"}) || m.Reason != "watch" {
			t.Fatalf("subscriber got %+v ok=%v", m, ok)
		}
	}
}

func TestLiveHubGateDropsEmit(t *testing.T) {
	t.Parallel()
	busy := true
	h := newLiveHub(config.RefreshConfig{Enabled: true}, true, func() bool { return busy })
	ch, cancel := h.subscribe()
	defer cancel()
	h.emit(liveMsg{Changed: []string{"status"}})
	if _, ok := recvLive(t, ch, 50*time.Millisecond); ok {
		t.Fatal("emit must be dropped while the gate is closed")
	}
	busy = false
	h.emit(liveMsg{Changed: []string{"status"}})
	if _, ok := recvLive(t, ch, time.Second); !ok {
		t.Fatal("emit must pass once the gate opens")
	}
}

func TestLiveHubFullBufferNeverBlocks(t *testing.T) {
	t.Parallel()
	h := newLiveHub(config.RefreshConfig{Enabled: true}, true, nil)
	_, cancel := h.subscribe() // never read
	defer cancel()
	done := make(chan struct{})
	go func() {
		for i := 0; i < liveSubBuffer*3; i++ {
			h.emit(liveMsg{Changed: []string{"feed"}})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emit blocked on a subscriber that never reads")
	}
}

func TestLiveHubCloseClosesSubscribers(t *testing.T) {
	t.Parallel()
	h := newLiveHub(config.RefreshConfig{Enabled: true}, true, nil)
	ch, cancel := h.subscribe()
	defer cancel()
	h.close()
	h.close() // idempotent
	if _, ok := <-ch; ok {
		t.Fatal("subscriber channel must be closed by close()")
	}
	h.emit(liveMsg{Changed: []string{"feed"}}) // must not panic after close
}

func TestWatchFanOutMirrorsTUI(t *testing.T) {
	t.Parallel()
	cases := map[gitwatch.Source][]string{
		gitwatch.Branches:  {"branches", "feed"},
		gitwatch.Remotes:   {"remotes", "feed", "branches"},
		gitwatch.Worktrees: {"worktrees", "branches", "feed"},
		gitwatch.Reflog:    {"reflog"},
	}
	for src, want := range cases {
		if got := watchFanOut(src); !slices.Equal(got, want) {
			t.Errorf("%s → %v, want %v", src, got, want)
		}
	}
}

func TestLiveHubWatchActive(t *testing.T) {
	t.Parallel()
	cfg := config.RefreshConfig{Enabled: true, BranchesWatch: true, ReflogWatch: true}
	h := newLiveHub(cfg, true, nil)
	if !h.watchActive("branches") || !h.watchActive("reflog") {
		t.Fatal("toggled-on eligible sources must be watch-active")
	}
	if h.watchActive("remotes") || h.watchActive("status") || h.watchActive("feed") {
		t.Fatal("off or ineligible sources must not be watch-active")
	}
	unsupported := newLiveHub(cfg, false, nil)
	if unsupported.watchActive("branches") {
		t.Fatal("unsupported filesystem disables watch")
	}
}

// --- ticker / watcher / lifecycle -------------------------------------------

// fakeClock drives liveNow in ticker tests. NOT parallel-safe (package seam).
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func useFakeClock(t *testing.T) *fakeClock {
	t.Helper()
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	prevNow, prevTick := liveNow, liveTick
	liveNow, liveTick = c.now, 5*time.Millisecond
	t.Cleanup(func() { liveNow, liveTick = prevNow, prevTick })
	return c
}

func writeRepoRefresh(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte("[refresh]\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLiveIntervalFloorAndOff(t *testing.T) {
	t.Parallel()
	cfg := config.RefreshConfig{Status: 3, Feed: 0, Branches: 60}
	if secs, on := liveInterval(cfg, "status"); !on || secs != liveMinSeconds {
		t.Fatalf("status = %d/%v, want floored to %d", secs, on, liveMinSeconds)
	}
	if _, on := liveInterval(cfg, "feed"); on {
		t.Fatal("0 must mean off")
	}
	if secs, _ := liveInterval(cfg, "branches"); secs != 60 {
		t.Fatalf("branches = %d, want 60", secs)
	}
	cfg.MinSeconds = 2
	if secs, _ := liveInterval(cfg, "status"); secs != 3 {
		t.Fatalf("min_seconds=2 must let 3 through, got %d", secs)
	}
}

func TestLiveTickerEmitsDueSource(t *testing.T) {
	clock := useFakeClock(t)
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\nstatus = 10\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ch, cancel := srv.liveHubRef().subscribe()
	defer cancel()
	if _, ok := recvLive(t, ch, 50*time.Millisecond); ok {
		t.Fatal("nothing is due right after start")
	}
	clock.advance(11 * time.Second)
	m, ok := recvLive(t, ch, 2*time.Second)
	if !ok || !slices.Equal(m.Changed, []string{"status"}) || m.Reason != "interval" {
		t.Fatalf("got %+v ok=%v, want changed=[status] reason=interval", m, ok)
	}
	if _, ok := recvLive(t, ch, 50*time.Millisecond); ok {
		t.Fatal("a source must not fire twice within one interval")
	}
}

func TestLiveTickerSkipsWatchActiveAndDisabled(t *testing.T) {
	clock := useFakeClock(t)
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	// branches is watch-active (when supported) → the ticker must not poll it.
	writeRepoRefresh(t, dir, "enabled = true\nbranches = 10\nbranches_watch = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	h := srv.liveHubRef()
	if !h.watchActive("branches") {
		t.Skip("file watch unsupported on this filesystem")
	}
	ch, cancel := h.subscribe()
	defer cancel()
	clock.advance(30 * time.Second)
	if m, ok := recvLive(t, ch, 200*time.Millisecond); ok {
		t.Fatalf("watch-active source polled by the ticker: %+v", m)
	}

	// Master switch off → no hub activity at all.
	writeRepoRefresh(t, dir, "enabled = false\nstatus = 10\n")
	srv.restartLive(context.Background())
	ch2, cancel2 := srv.liveHubRef().subscribe()
	defer cancel2()
	clock.advance(30 * time.Second)
	if m, ok := recvLive(t, ch2, 200*time.Millisecond); ok {
		t.Fatalf("disabled refresh still emitted %+v", m)
	}
}

func TestLiveWatchEmitsOnCommit(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\nbranches_watch = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	h := srv.liveHubRef()
	if !h.watchActive("branches") {
		t.Skip("file watch unsupported on this filesystem")
	}
	ch, cancel := h.subscribe()
	defer cancel()
	time.Sleep(100 * time.Millisecond) // let fsnotify register the dirs
	gitRun(t, dir, "commit", "--allow-empty", "-m", "external")
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-ch:
			if slices.Contains(m.Changed, "branches") && slices.Contains(m.Changed, "feed") && m.Reason == "watch" {
				return
			}
		case <-deadline:
			t.Fatal("no watch event for an external commit")
		}
	}
}

func TestLiveStopLiveClosesStreams(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\nstatus = 10\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	ch, cancel := srv.liveHubRef().subscribe()
	defer cancel()
	srv.stopLive()
	if _, ok := <-ch; ok {
		t.Fatal("stopLive must close subscriber channels")
	}
	if srv.liveHubRef() != nil {
		t.Fatal("stopLive must clear the hub")
	}
	srv.Close() // idempotent
}

// --- /api/events --------------------------------------------------------------

// readLiveSSE opens /api/events and returns the first n data messages.
func readLiveSSE(t *testing.T, ts *httptest.Server, n int, timeout time.Duration) []liveMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("status=%d content-type=%s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var out []liveMsg
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var m liveMsg
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
			t.Fatalf("bad SSE json %q: %v", line, err)
		}
		out = append(out, m)
		if len(out) == n {
			return out
		}
	}
	t.Fatalf("stream ended after %d messages (want %d): %v", len(out), n, sc.Err())
	return nil
}

func TestEventsHelloReportsLiveAndWatch(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\nbranches_watch = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	hello := readLiveSSE(t, ts, 1, 3*time.Second)[0]
	if hello.Reason != "hello" || hello.Live == nil || !*hello.Live || hello.Watch == nil {
		t.Fatalf("hello = %+v", hello)
	}
	if *hello.Watch != srv.liveHubRef().watchActive("branches") {
		t.Fatalf("hello.watch=%v, hub watch-active=%v", *hello.Watch, srv.liveHubRef().watchActive("branches"))
	}
}

func TestEventsHelloWhenNotStarted(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir))) // startLive never called (handler tests)
	hello := readLiveSSE(t, ts, 1, 3*time.Second)[0]
	if hello.Reason != "hello" || hello.Live == nil || *hello.Live {
		t.Fatalf("hello without a hub must say live=false: %+v", hello)
	}
}

func TestEventsStreamsHubEmits(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	go func() {
		time.Sleep(200 * time.Millisecond) // after the subscriber is attached
		srv.liveHubRef().emit(liveMsg{Changed: []string{"feed"}, Reason: "watch"})
	}()
	msgs := readLiveSSE(t, ts, 2, 3*time.Second)
	if !slices.Equal(msgs[1].Changed, []string{"feed"}) {
		t.Fatalf("second message = %+v, want changed=[feed]", msgs[1])
	}
}

func TestRerootRestartsLive(t *testing.T) {
	isolateGlobal(t)
	a := newRepoDir(t, 1)
	b := newRepoDir(t, 2)
	writeRepoRefresh(t, a, "enabled = true\nstatus = 10\n")
	writeRepoRefresh(t, b, "enabled = true\nstatus = 20\n")
	srv := New(domain.Open(a))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	before := srv.liveHubRef()
	chOld, cancel := before.subscribe()
	defer cancel()
	var out map[string]any
	pb, _ := json.Marshal(b)
	if code := postJSON(t, ts, "/api/reroot", `{"path":`+string(pb)+`}`, "application/json", ts.URL, &out); code != http.StatusOK {
		t.Fatalf("reroot code = %d: %v", code, out)
	}
	after := srv.liveHubRef()
	if after == nil || after == before {
		t.Fatal("reroot must build a fresh hub for the new root")
	}
	if _, ok := <-chOld; ok {
		t.Fatal("old hub's streams must be closed on reroot")
	}
	if after.cfg.Status != 20 {
		t.Fatalf("new hub cfg.Status = %d, want the new root's 20", after.cfg.Status)
	}
}
