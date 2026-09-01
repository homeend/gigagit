# Web Live Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg web` refreshes itself on repo changes — a server-side gitwatch watcher + interval ticker broadcasts "sources changed" over one persistent SSE endpoint, the page re-fetches those sources, and the web settings panel exposes the four `[refresh] *_watch` toggles.

**Architecture:** A `liveHub` in `internal/web/live.go` owns subscribers, the `gitwatch.Watcher`, and a 1 s ticker; it emits `liveMsg{changed, reason}` unless an op is in flight. `GET /api/events` streams those as SSE. `internal/web/static/live.js` subscribes, coalesces messages, and re-fetches through the `runOnce("refresh")` gate. The hub restarts on re-root and on any `[refresh]` settings write.

**Tech Stack:** Go 1.26, `internal/gitwatch` (fsnotify), net/http SSE, vanilla ES modules, real-git tests in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-09-02-web-live-refresh-design.md`

## Global Constraints

- Worktree: all work in `/mnt/t/others/gigagit/.claude/worktrees/web-live` (branch `feat/web-live-refresh`); prefix shell commands with `cd` to it and use absolute paths.
- `internal/web` may import `internal/gitwatch`, `internal/config`, `internal/domain`, `internal/engine`; never `internal/git`, `internal/tui`, `internal/i18n`. English-only strings.
- Wire source vocabulary (exact): `status, branches, remotes, worktrees, tags, reflog, feed`; watch-eligible: `worktrees, branches, reflog, remotes`; interval-only actions: `fetch, remote_tags`.
- Interval floor: `min_seconds` (default 10); `0` = off. Watch debounce 200 ms. SSE keepalive 25 s. Subscriber buffer 8.
- Tests that set the package seams `liveTick` / `liveNow` must NOT call `t.Parallel()`.
- Every commit message ends with:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5
  ```
- Run `gofmt -l internal/` and `go vet ./internal/web/` before every commit.

---

## File map

| File | Responsibility |
|---|---|
| `internal/web/live.go` (create) | `liveHub` (subscribe/emit/close), wire vocabulary + watch fan-out, `Server.opInFlight`, `startLive/stopLive/restartLive/Close`, watcher loop, ticker, `handleEvents` |
| `internal/web/live_test.go` (create) | hub unit tests, ticker with fake clock, watch end-to-end, SSE handler, op suppression, reroot restart |
| `internal/web/server.go` (modify) | two fields on `Server`: `liveMu sync.Mutex; live *liveHub` |
| `internal/web/serve.go` (modify) | start live after `New`, `Close()` after `Shutdown` |
| `internal/web/reroot.go` (modify) | restart live after the service swap |
| `internal/web/settings.go` (modify) | `refresh_watch` + `watch_supported` on GET; `refresh_watch` on POST → `config.SetRefreshWatch`; restart live after refresh writes |
| `internal/web/settings_test.go` (modify) | watch round-trip test |
| `internal/web/static/settings.js` (modify) | file-watch toggles, drop `(TUI)` on the refresh heading, footnote |
| `internal/web/static/live.js` (create) | EventSource client, coalescing, source→fetch map |
| `internal/web/static/app.js` (modify) | import + `connectLive()` at the end of `boot()` |
| `CHANGELOG.md`, `docs/web-tui-parity.md`, `README.md` | docs |

---

### Task 1: liveHub core + op-in-flight helper

**Files:**
- Create: `internal/web/live.go`
- Create: `internal/web/live_test.go`
- Modify: `internal/web/server.go:27-59` (add fields)

**Interfaces:**
- Produces:
  - `type liveMsg struct { Changed []string; Reason string; Live *bool; Watch *bool }` (json tags `changed`, `reason`, `live,omitempty`, `watch,omitempty`)
  - `func newLiveHub(cfg config.RefreshConfig, watchOK bool, gate func() bool) *liveHub`
  - `func (h *liveHub) subscribe() (<-chan liveMsg, func())`
  - `func (h *liveHub) emit(msg liveMsg)` — dropped when `gate()` is true; non-blocking per subscriber
  - `func (h *liveHub) close()` — idempotent; closes `h.stop` and every subscriber channel
  - `func watchFanOut(s gitwatch.Source) []string`
  - `func (h *liveHub) watchActive(src string) bool`
  - `func (s *Server) opInFlight() bool`
  - `Server` fields `liveMu sync.Mutex`, `live *liveHub`

- [ ] **Step 1: Write the failing tests**

```go
// internal/web/live_test.go
package web

import (
	"slices"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go test ./internal/web/ -run 'TestLiveHub|TestWatchFanOut' 2>&1 | tail -5`
Expected: build failure, `undefined: newLiveHub`.

- [ ] **Step 3: Implement the hub**

```go
// internal/web/live.go
package web

import (
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/gitwatch"
)

// Live refresh — the web analog of the TUI's refresh registry.
//
// A liveHub owns everything that can say "the repo changed" without a request
// asking: the gitwatch watcher (refs, reflog, worktrees; fsnotify, debounced)
// and the interval ticker (every other source, plus the fetch / remote-tags
// actions). Both feed emit(), which fans a liveMsg out to every subscriber —
// one per open /api/events stream. The vocabulary on the wire is the TUI's
// [refresh] key set, so a browser and a terminal on the same repo refresh the
// same things for the same reasons.
//
// Emits are dropped while an op is in flight (the gate): the op's own
// post-op refresh reloads everything, and a watcher that fires on the op's
// own ref writes would only race it.

const (
	liveDebounce   = 200 * time.Millisecond // gitwatch per-source debounce (TUI's watchDebounce)
	liveKeepalive  = 25 * time.Second       // SSE comment ping so idle streams stay open
	liveSubBuffer  = 8                      // per-subscriber channel depth; a full one drops
	liveMinSeconds = 10                     // floor on any interval (TUI's defaultMinSeconds)
)

// Test seams: the ticker period and the clock the ticker reads.
var (
	liveTick = time.Second
	liveNow  = time.Now
)

// liveSources is the wire vocabulary in ticker order (the TUI's
// scheduledItems: refreshTomlKey). fetch and remote_tags are actions the
// ticker RUNS; they are never emitted as changed sources.
var liveSources = []string{"status", "branches", "remotes", "worktrees", "tags", "reflog", "feed", "fetch", "remote_tags"}

// liveWatchSources are the watch-eligible sources (gitwatch's four).
var liveWatchSources = []string{"worktrees", "branches", "reflog", "remotes"}

// liveMsg is one SSE payload. Live/Watch ride only on the hello message.
type liveMsg struct {
	Changed []string `json:"changed"`
	Reason  string   `json:"reason"`
	Live    *bool    `json:"live,omitempty"`
	Watch   *bool    `json:"watch,omitempty"`
}

type liveHub struct {
	mu      sync.Mutex
	subs    map[chan liveMsg]struct{}
	stopped bool
	stop    chan struct{} // closed by close(); the watcher/ticker goroutines exit on it
	cfg     config.RefreshConfig
	watchOK bool          // gitwatch.Supported(commonDir)
	gate    func() bool   // true = an op is in flight → drop
	watcher *gitwatch.Watcher
	lastRun map[string]time.Time
}

func newLiveHub(cfg config.RefreshConfig, watchOK bool, gate func() bool) *liveHub {
	h := &liveHub{
		subs:    map[chan liveMsg]struct{}{},
		stop:    make(chan struct{}),
		cfg:     cfg,
		watchOK: watchOK,
		gate:    gate,
		lastRun: map[string]time.Time{},
	}
	// Seed every source as "just refreshed": the page that connects has just
	// loaded everything, so the first interval poll waits a full interval.
	now := liveNow()
	for _, src := range liveSources {
		h.lastRun[src] = now
	}
	return h
}

// subscribe returns a channel that receives every emit until cancel (or
// close). Buffered: a slow reader drops messages rather than stalling emit.
func (h *liveHub) subscribe() (<-chan liveMsg, func()) {
	ch := make(chan liveMsg, liveSubBuffer)
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// emit fans msg out to every subscriber, non-blocking. Dropped whole while
// the gate reports an op in flight.
func (h *liveHub) emit(msg liveMsg) {
	if h.gate != nil && h.gate() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // this tab's buffer is full — drop; its reconnect refreshes in full
		}
	}
}

// close stops the watcher and ticker and closes every subscriber channel so
// the streams end and browsers reconnect (to whichever hub replaces this one).
func (h *liveHub) close() {
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	h.stopped = true
	close(h.stop)
	w := h.watcher
	h.watcher = nil
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
}

// watchActive reports whether src is driven by the file watcher: eligible,
// toggled on, and the filesystem supports it. Watch-active sources are
// skipped by the interval ticker (the TUI's dueItems rule).
func (h *liveHub) watchActive(src string) bool {
	if !h.watchOK {
		return false
	}
	switch src {
	case "worktrees":
		return h.cfg.WorktreesWatch
	case "branches":
		return h.cfg.BranchesWatch
	case "reflog":
		return h.cfg.ReflogWatch
	case "remotes":
		return h.cfg.RemotesWatch
	}
	return false
}

// watchFanOut maps a watcher source to the wire sources it invalidates — the
// TUI's watchAffectedSources table, primary first: a ref move changes the
// feed's decorations and tip markers; a remote move changes branches'
// upstream/ahead/behind; a worktree change re-marks branches.
func watchFanOut(s gitwatch.Source) []string {
	switch s {
	case gitwatch.Branches:
		return []string{"branches", "feed"}
	case gitwatch.Remotes:
		return []string{"remotes", "feed", "branches"}
	case gitwatch.Worktrees:
		return []string{"worktrees", "branches", "feed"}
	case gitwatch.Reflog:
		return []string{"reflog"}
	}
	return nil
}

// opInFlight reports whether the op transport has a live (not done) run —
// the idiom handleReroot and startOp repeat inline.
func (s *Server) opInFlight() bool {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.cur == nil {
		return false
	}
	s.cur.mu.Lock()
	live := !s.cur.done
	s.cur.mu.Unlock()
	return live
}
```

Add to `Server` in `internal/web/server.go` right after the `packThreshold` field:

```go
	// live refresh (live.go): the watcher + ticker hub behind GET /api/events.
	// nil until startLive; replaced wholesale on re-root and refresh-settings
	// writes (subscribers are closed and reconnect).
	liveMu sync.Mutex
	live   *liveHub
```

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go vet ./internal/web/ && go test ./internal/web/ -run 'TestLiveHub|TestWatchFanOut' -count=1 2>&1 | tail -3`
Expected: 6 passed.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-live && gofmt -l internal/ && git add internal/web/live.go internal/web/live_test.go internal/web/server.go && git commit -m "feat(web): liveHub — subscriber fan-out for repo-change events

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5"
```

---

### Task 2: interval ticker + watcher loop + start/stop lifecycle

**Files:**
- Modify: `internal/web/live.go`
- Modify: `internal/web/live_test.go`

**Interfaces:**
- Consumes: Task 1's hub; `(*Server).effectiveConfig(ctx, svc) (config.Config, error)` (`review.go:317`); `svc.GitCommonDir(ctx)`, `svc.GitDir(ctx)`, `svc.RemoteTagsFresh(ctx)`, `svc.Execute(ctx, engine.Fetch{}, events, engine.MapDecider{})`.
- Produces:
  - `func (s *Server) startLive(ctx context.Context)` — builds a hub from the effective config, starts watcher + ticker goroutines, stores it (closing any previous)
  - `func (s *Server) stopLive()` / `func (s *Server) restartLive(ctx)` / `func (s *Server) Close()` (= stopLive)
  - `func (s *Server) liveHubRef() *liveHub` (nil when not started)
  - `func (h *liveHub) tickOnce(svc *domain.Service)` — one due-check pass (exported for tests via the seams)
  - `func liveInterval(cfg config.RefreshConfig, src string) (secs int, on bool)`

- [ ] **Step 1: Write the failing tests** (append to `live_test.go`; add imports `context`, `os`, `path/filepath`, `github.com/homeend/gigagit/internal/domain`)

```go
// fakeClock drives liveNow in ticker tests. NOT parallel-safe (package seam).
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
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
	if !h.watchOK {
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
	t.Parallel()
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\nbranches_watch = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	h := srv.liveHubRef()
	if !h.watchOK {
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
	t.Parallel()
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
```

Note: `isolateGlobal` uses `t.Setenv`, which is incompatible with `t.Parallel()` — remove `t.Parallel()` from `TestLiveWatchEmitsOnCommit` and `TestLiveStopLiveClosesStreams` if the test runner complains ("testing: t.Parallel called after t.Setenv"). Prefer dropping `t.Parallel()` on all four Server-level tests here.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go test ./internal/web/ -run 'TestLive' 2>&1 | tail -5`
Expected: `undefined: liveInterval` / `startLive`.

- [ ] **Step 3: Implement** (append to `live.go`; add imports `context`, `github.com/homeend/gigagit/internal/domain`, `github.com/homeend/gigagit/internal/engine`)

```go
// liveInterval returns src's poll interval in seconds and whether it is
// scheduled: 0 = off, otherwise floored at min_seconds (default 10). The
// TUI's scheduledInterval.
func liveInterval(cfg config.RefreshConfig, src string) (int, bool) {
	var base int
	switch src {
	case "status":
		base = cfg.Status
	case "branches":
		base = cfg.Branches
	case "remotes":
		base = cfg.Remotes
	case "worktrees":
		base = cfg.Worktrees
	case "tags":
		base = cfg.Tags
	case "reflog":
		base = cfg.Reflog
	case "feed":
		base = cfg.Feed
	case "fetch":
		base = cfg.Fetch
	case "remote_tags":
		base = cfg.RemoteTags
	}
	if base <= 0 {
		return 0, false
	}
	min := cfg.MinSeconds
	if min <= 0 {
		min = liveMinSeconds
	}
	if base < min {
		base = min
	}
	return base, true
}

// liveHubRef returns the current hub (nil before startLive / after stopLive).
func (s *Server) liveHubRef() *liveHub {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	return s.live
}

// startLive builds the hub for the CURRENT service from the effective
// [refresh] config and starts its watcher and ticker. Any previous hub is
// closed first (its streams end; browsers reconnect to this one). Safe to
// call when refresh is disabled: the hub then only answers hello messages.
func (s *Server) startLive(ctx context.Context) {
	svc := s.service()
	cfg := config.RefreshConfig{}
	if c, err := s.effectiveConfig(ctx, svc); err == nil {
		cfg = c.Refresh
	}
	common, err := svc.GitCommonDir(ctx)
	watchOK := err == nil && common != "" && gitwatch.Supported(common)
	h := newLiveHub(cfg, watchOK, s.opInFlight)

	s.liveMu.Lock()
	prev := s.live
	s.live = h
	s.liveMu.Unlock()
	if prev != nil {
		prev.close()
	}
	if !cfg.Enabled {
		return
	}
	// Watcher: only the eligible sources toggled on, only when the fs can.
	var enabled []gitwatch.Source
	if watchOK {
		for src, gs := range map[string]gitwatch.Source{
			"worktrees": gitwatch.Worktrees, "reflog": gitwatch.Reflog,
			"branches": gitwatch.Branches, "remotes": gitwatch.Remotes,
		} {
			if h.watchActive(src) {
				enabled = append(enabled, gs)
			}
		}
	}
	if len(enabled) > 0 {
		worktreeDir, werr := svc.GitDir(ctx)
		if werr != nil || worktreeDir == "" {
			worktreeDir = common // main worktree: logs/HEAD lives at common
		}
		if w, werr := gitwatch.New(gitwatch.Plan(common, worktreeDir, enabled), liveDebounce); werr == nil {
			h.mu.Lock()
			h.watcher = w
			h.mu.Unlock()
			go h.watchLoop(w)
		}
		// A failed watcher construction leaves watchOK true but no watcher:
		// the ticker still skips those sources (as the TUI does — watch on
		// means no interval backstop). Flip watchOK so polling covers them.
		if h.watcher == nil {
			h.mu.Lock()
			h.watchOK = false
			h.mu.Unlock()
		}
	}
	go h.tickLoop(svc)
}

// stopLive closes the hub (streams end, watcher/ticker exit).
func (s *Server) stopLive() {
	s.liveMu.Lock()
	h := s.live
	s.live = nil
	s.liveMu.Unlock()
	if h != nil {
		h.close()
	}
}

// restartLive re-reads config for the current service — the TUI's watchGen
// bump after a refresh-settings write.
func (s *Server) restartLive(ctx context.Context) { s.startLive(ctx) }

// Close releases background resources (the live hub). Serve calls it after
// the HTTP server has shut down.
func (s *Server) Close() { s.stopLive() }

// watchLoop forwards watcher events as fan-out emits until the watcher or
// the hub stops.
func (h *liveHub) watchLoop(w *gitwatch.Watcher) {
	for {
		select {
		case src, ok := <-w.Events():
			if !ok {
				return
			}
			h.emit(liveMsg{Changed: watchFanOut(src), Reason: "watch"})
		case <-h.stop:
			return
		}
	}
}

// tickLoop runs tickOnce every liveTick until the hub stops. One lane: a
// slow fetch simply delays the next due-check, it never overlaps it.
func (h *liveHub) tickLoop(svc *domain.Service) {
	t := time.NewTicker(liveTick)
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-t.C:
			h.tickOnce(svc)
		}
	}
}

// tickOnce runs every due interval source once: fetch and remote_tags do
// their network work here (server-side, like the TUI's background lane) and
// then emit the sources they changed; every other source just emits itself.
// Skipped whole while an op is in flight. lastRun is stamped even when the
// action fails, so a dead remote is not hammered every tick.
func (h *liveHub) tickOnce(svc *domain.Service) {
	if h.gate != nil && h.gate() {
		return
	}
	h.mu.Lock()
	cfg, stopped := h.cfg, h.stopped
	h.mu.Unlock()
	if stopped || !cfg.Enabled {
		return
	}
	now := liveNow()
	for _, src := range liveSources {
		secs, on := liveInterval(cfg, src)
		if !on || h.watchActive(src) {
			continue
		}
		h.mu.Lock()
		due := now.Sub(h.lastRun[src]) >= time.Duration(secs)*time.Second
		if due {
			h.lastRun[src] = now
		}
		h.mu.Unlock()
		if !due {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		switch src {
		case "fetch":
			events := make(chan engine.Event, 8)
			go func() {
				for range events {
				}
			}()
			_, _ = svc.Execute(ctx, engine.Fetch{}, events, engine.MapDecider{})
			close(events)
			h.emit(liveMsg{Changed: []string{"remotes", "branches", "feed"}, Reason: "interval"})
		case "remote_tags":
			_, _ = svc.RemoteTagsFresh(ctx)
			h.emit(liveMsg{Changed: []string{"tags"}, Reason: "interval"})
		default:
			h.emit(liveMsg{Changed: []string{src}, Reason: "interval"})
		}
		cancel()
	}
}
```

`watchActive` must now read `h.watchOK` under `h.mu` (it is flipped in startLive). Update it:

```go
func (h *liveHub) watchActive(src string) bool {
	h.mu.Lock()
	ok, cfg := h.watchOK, h.cfg
	h.mu.Unlock()
	if !ok {
		return false
	}
	switch src {
	case "worktrees":
		return cfg.WorktreesWatch
	case "branches":
		return cfg.BranchesWatch
	case "reflog":
		return cfg.ReflogWatch
	case "remotes":
		return cfg.RemotesWatch
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go vet ./internal/web/ && go test ./internal/web/ -run 'TestLive|TestWatchFanOut' -count=1 -race 2>&1 | tail -5`
Expected: all pass (the watch test may SKIP on 9p).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-live && gofmt -l internal/ && git add internal/web/live.go internal/web/live_test.go && git commit -m "feat(web): live hub watcher loop, interval ticker and lifecycle

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5"
```

---

### Task 3: `GET /api/events` SSE endpoint

**Files:**
- Modify: `internal/web/live.go` (handler + route registration)
- Modify: `internal/web/live_test.go`

**Interfaces:**
- Consumes: Task 2 hub; `writeSSE(w, wireEvent)` pattern from `ophttp.go:1074` (write our own `writeLiveSSE` since the payload is a struct).
- Produces: route `GET /api/events`; first message `{"changed":[],"reason":"hello","live":bool,"watch":bool}`; then `liveMsg`s; `: ping` comment every 25 s.

- [ ] **Step 1: Write the failing tests** (append to `live_test.go`; add imports `bufio`, `encoding/json`, `net/http`, `strings`)

```go
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
	msgs := readLiveSSE(t, ts, 1, 3*time.Second)
	hello := msgs[0]
	if hello.Reason != "hello" || hello.Live == nil || !*hello.Live || hello.Watch == nil {
		t.Fatalf("hello = %+v", hello)
	}
	if *hello.Watch != srv.liveHubRef().watchOK {
		t.Fatalf("hello.watch=%v, hub watchOK=%v", *hello.Watch, srv.liveHubRef().watchOK)
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go test ./internal/web/ -run 'TestEvents' -count=1 2>&1 | tail -5`
Expected: FAIL — status 404 (route missing).

- [ ] **Step 3: Implement** (append to `live.go`; add imports `encoding/json`, `errors`, `fmt`, `net/http`)

```go
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/events", s.handleEvents)
	})
}

// handleEvents is the persistent repo-change stream. First message is a
// hello carrying whether refresh is enabled and whether file watch is
// active; then one message per hub emit; a comment ping every liveKeepalive
// keeps idle streams open. Ends when the client goes away or the hub is
// replaced (re-root, settings write) — EventSource reconnects and gets a
// fresh hello, which is how a tab learns the new state.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	h := s.liveHubRef()
	live, watch := false, false
	var ch <-chan liveMsg
	cancel := func() {}
	if h != nil {
		h.mu.Lock()
		live = h.cfg.Enabled
		watch = live && h.watcher != nil
		h.mu.Unlock()
		ch, cancel = h.subscribe()
	}
	defer cancel()
	writeLiveSSE(w, liveMsg{Changed: []string{}, Reason: "hello", Live: &live, Watch: &watch})
	fl.Flush()

	ping := time.NewTicker(liveKeepalive)
	defer ping.Stop()
	for {
		select {
		case m, ok := <-ch:
			if !ok {
				return // hub replaced or stopped; the browser reconnects
			}
			writeLiveSSE(w, m)
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeLiveSSE(w http.ResponseWriter, m liveMsg) {
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
}
```

Note: when `h == nil`, `ch` is a nil channel — `select` never fires on it, so the loop idles on pings until the client disconnects. That is intended.

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go vet ./internal/web/ && go test ./internal/web/ -run 'TestEvents|TestLive' -count=1 -race 2>&1 | tail -3`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-live && gofmt -l internal/ && git add internal/web/live.go internal/web/live_test.go && git commit -m "feat(web): GET /api/events streams repo-change notifications

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5"
```

---

### Task 4: lifecycle wiring — serve, re-root

**Files:**
- Modify: `internal/web/serve.go:47-60`
- Modify: `internal/web/reroot.go:176-186`
- Modify: `internal/web/live_test.go`

**Interfaces:**
- Consumes: `startLive`, `Close`, `liveHubRef` (Task 2).

- [ ] **Step 1: Write the failing test** (append to `live_test.go`; look at `reroot_test.go` for how an existing test POSTs `/api/reroot` — copy its body shape; the origin arg to `postJSON` must be the test server's URL because reroot is `writeGuard`ed)

```go
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
	body := `{"path":` + strconvQuote(b) + `}`
	if code := postJSON(t, ts, "/api/reroot", body, "application/json", ts.URL, &out); code != http.StatusOK {
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

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
```

Check the reroot request body field name first: `grep -n 'json:"' internal/web/reroot.go | head` — use the real field (e.g. `path`).

- [ ] **Step 2: Run to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go test ./internal/web/ -run TestRerootRestartsLive -count=1 2>&1 | tail -3`
Expected: FAIL "reroot must build a fresh hub".

- [ ] **Step 3: Wire it**

In `internal/web/reroot.go`, right after
```go
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
```
add
```go
	// The watcher points at the OLD .git; rebuild the live hub for the new
	// root (streams close, tabs reconnect and re-hello).
	s.restartLive(r.Context())
```

In `internal/web/serve.go`, replace
```go
	httpSrv := &http.Server{Handler: New(svc).Handler()}
```
with
```go
	srv := New(svc)
	srv.startLive(ctx) // watcher + interval ticker behind GET /api/events
	defer srv.Close()
	httpSrv := &http.Server{Handler: srv.Handler()}
```
(`ctx` here is the function's incoming context — check the signature; use `context.Background()` if the incoming ctx is cancelled by the signal handler before Serve returns. `startLive` only uses ctx for the config/git-dir reads, so either is fine.)

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go vet ./internal/web/ && go test ./internal/web/ -run 'TestReroot|TestLive|TestEvents' -count=1 2>&1 | tail -3`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-live && gofmt -l internal/ && git add internal/web/serve.go internal/web/reroot.go internal/web/live_test.go && git commit -m "feat(web): start the live hub with the server and rebuild it on re-root

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5"
```

---

### Task 5: settings wire — `refresh_watch` + `watch_supported` (option 1, server half)

**Files:**
- Modify: `internal/web/settings.go:31-50, 78-89, 111-121, 150-169, 212-217`
- Modify: `internal/web/settings_test.go`

**Interfaces:**
- Produces: GET `/api/settings` gains `"refresh_watch": {"worktrees":bool,"branches":bool,"reflog":bool,"remotes":bool}` and `"watch_supported": bool`; POST accepts `"refresh_watch": {src: bool}` (keys validated against the four); any write touching `auto_refresh`, `refresh`, or `refresh_watch` calls `s.restartLive(r.Context())`.

- [ ] **Step 1: Write the failing test** (append to `settings_test.go`; extend `settingsGet` with `RefreshWatch map[string]bool \`json:"refresh_watch"\`` and `WatchSupported bool \`json:"watch_supported"\``)

```go
func TestSettingsRefreshWatchRoundTrip(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	got := getSettings(t, ts)
	if len(got.RefreshWatch) != 4 || got.RefreshWatch["branches"] {
		t.Fatalf("refresh_watch defaults = %v, want 4 false entries", got.RefreshWatch)
	}
	body := `{"refresh_watch":{"branches":true,"reflog":true}}`
	if code := postJSON(t, ts, "/api/settings", body, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST code = %d", code)
	}
	repo, err := os.ReadFile(filepath.Join(dir, ".gg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"branches_watch = true", "reflog_watch = true"} {
		if !strings.Contains(string(repo), want) {
			t.Errorf("repo config missing %q:\n%s", want, repo)
		}
	}
	got = getSettings(t, ts)
	if !got.RefreshWatch["branches"] || !got.RefreshWatch["reflog"] || got.RefreshWatch["remotes"] {
		t.Fatalf("refresh_watch after write = %v", got.RefreshWatch)
	}
	if code := postJSON(t, ts, "/api/settings", `{"refresh_watch":{"status":true}}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("ineligible watch source must be 400, got %d", code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go test ./internal/web/ -run TestSettingsRefreshWatchRoundTrip -count=1 2>&1 | tail -3`
Expected: FAIL "refresh_watch defaults = map[]".

- [ ] **Step 3: Implement**

In `settingsPayload` add after `Refresh`:
```go
	RefreshWatch   map[string]bool `json:"refresh_watch"`   // [refresh] <src>_watch, watch-eligible sources only
	WatchSupported bool            `json:"watch_supported"` // gitwatch.Supported(commonDir)
```
Populate in `handleSettings` after the `Refresh:` map:
```go
		RefreshWatch: map[string]bool{
			"worktrees": cfg.Refresh.WorktreesWatch, "branches": cfg.Refresh.BranchesWatch,
			"reflog": cfg.Refresh.ReflogWatch, "remotes": cfg.Refresh.RemotesWatch,
		},
```
and after the struct literal:
```go
	if common, cerr := svc.GitCommonDir(ctx); cerr == nil && common != "" {
		p.WatchSupported = gitwatch.Supported(common)
	}
```
(import `github.com/homeend/gigagit/internal/gitwatch`).

In `settingsWriteRequest` add:
```go
	RefreshWatch map[string]*bool `json:"refresh_watch"` // watch-eligible source → on/off
```
Validation, after the `req.Refresh` loop:
```go
	for src, on := range req.RefreshWatch {
		if !slices.Contains(liveWatchSources, src) {
			writeErr(w, http.StatusBadRequest, errors.New("not a file-watch source: "+src))
			return
		}
		if on == nil {
			writeErr(w, http.StatusBadRequest, errors.New("refresh_watch values must be booleans"))
			return
		}
	}
```
Extend the "nothing to set" guard with `&& len(req.RefreshWatch) == 0` and `needRepo` with `|| len(req.RefreshWatch) > 0`.
Write, after the `req.Refresh` interval loop:
```go
	for src, on := range req.RefreshWatch {
		if err := config.SetRefreshWatch(repoPath, src, *on); err != nil {
			fail(err)
			return
		}
	}
```
At the end, before `writeJSON(w, map[string]bool{"ok": true})`:
```go
	if req.AutoRefresh != nil || len(req.Refresh) > 0 || len(req.RefreshWatch) > 0 {
		s.restartLive(r.Context()) // the hub re-reads [refresh]; tabs re-hello
	}
```
(import `slices`.)

- [ ] **Step 4: Run tests**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go vet ./internal/web/ && go test ./internal/web/ -run 'TestSettings' -count=1 2>&1 | tail -3`
Expected: all pass (existing `TestSettingsGetDefaults` still expects 9 interval keys — unchanged).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-live && gofmt -l internal/ && git add internal/web/settings.go internal/web/settings_test.go && git commit -m "feat(web): settings carry the [refresh] file-watch toggles

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5"
```

---

### Task 6: settings panel UI (option 1, client half)

**Files:**
- Modify: `internal/web/static/settings.js:23, 173-187, 217-225`

**Interfaces:**
- Consumes: Task 5 payload (`refresh_watch`, `watch_supported`).
- Produces: toggle buttons with `data-k="watch:<src>"`; POST `{"refresh_watch":{src: bool}}`.

- [ ] **Step 1: Render the toggles**

After the `REFRESH_SOURCES` const add:
```js
// The watch-eligible [refresh] sources — the ones gitwatch can drive from
// .git changes instead of a timer (the server's liveWatchSources).
const WATCH_SOURCES = ["worktrees", "branches", "reflog", "remotes"];
```
Replace the `rates` builder with one that appends a file-watch toggle on eligible rows:
```js
  const rates = REFRESH_SOURCES.map((src) => {
    const watch = WATCH_SOURCES.includes(src)
      ? ` <span class="swatch">file watch ${toggleBtn("watch:" + src, !!(d.refresh_watch || {})[src])}</span>`
      : "";
    return `<label class="srate"><span>${esc(src.replace("_", " "))}</span><input type="text" inputmode="numeric" data-rate="${src}" value="${d.refresh[src] ?? 0}"></label>${watch}`;
  }).join("");
  const watchNote = d.watch_supported
    ? "file watch: refresh on .git change instead of the interval"
    : "file watch unsupported on this filesystem (9p) — the interval is used";
```
Replace the refresh heading + footnote lines:
```js
    <h3>refresh</h3>
    ...
    <div class="srow"><span class="snote">0 = off · per repo · applies to the TUI and to this page · ${watchNote}</span></div>
```
(keep the `auto-refresh` / `auto remote-tag refresh` / `intervals (s)` rows as they are).

- [ ] **Step 2: Route the click**

In the delegated click handler, before `if (t.dataset.k) {`:
```js
  if (t.dataset.k && t.dataset.k.startsWith("watch:")) {
    const src = t.dataset.k.slice(6);
    const cur = !!(state.settings.refresh_watch || {})[src];
    setOpt({ refresh_watch: { [src]: !cur } });
    return;
  }
```

- [ ] **Step 3: Style** — in `internal/web/static/style.css` find `.srate` and add beside it:
```css
.swatch { display: inline-flex; align-items: center; gap: 6px; margin-left: 8px; color: var(--muted, #888); font-size: 12px; }
```
(if there is no `--muted` var, use the color the existing `.snote` rule uses — `grep -n "\.snote" internal/web/static/style.css`).

- [ ] **Step 4: Verify by build + browser**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go build -o /mnt/t/others/gigagit/.claude/worktrees/web-live/gg ./cmd/gg`
Then with the scratchpad Playwright setup (`/tmp/claude-1000/-mnt-t-others-gigagit/5f4b8b8b-80ab-435d-b981-95eafae40707/scratchpad/pw`, `node_modules/playwright` present): spawn `gg web --addr 127.0.0.1:<port>` in a throwaway repo, open the page, press `,` (or open ☰ → settings — check `keys.js` for the settings key), assert four `button[data-k^="watch:"]` exist, click the branches one, re-read `/api/settings` and assert `refresh_watch.branches === true`. Screenshot to `settings-watch.png` and Read it.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-live && git add internal/web/static/settings.js internal/web/static/style.css && git commit -m "feat(web): file-watch toggles in the settings refresh section

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5"
```

---

### Task 7: client `live.js` — subscribe and re-fetch

**Files:**
- Create: `internal/web/static/live.js`
- Modify: `internal/web/static/app.js:39, 73-75`

**Interfaces:**
- Consumes: `runOnce`, `state` (core.js); `fetchStatus`, `wtCount` (status.js); `reconcileStatusView` (files.js); `fetchBranches` (sidebar.js); `loadCommits`, `renderCommits` (commits.js); `loadRepo`, `refreshAfterOp` (ops.js). Import order in app.js: `live.js` last (after `remoteheads.js`), so every module it imports has already run its top-level code.
- Produces: `connectLive()`; `state.live = {enabled, watch}`.

- [ ] **Step 1: Write live.js**

```js
// live.js — the page's half of live refresh: one EventSource on
// /api/events, coalesced into a single re-fetch of just the sources the
// server named. The server decides WHEN (file watch, intervals, the
// [refresh] config); this module only decides WHAT to reload for a name.
//
// Rules: never while an op is running (its own post-op refresh covers
// everything); never two refreshes at once (the runOnce("refresh") gate
// manualRefresh uses too — an `r` press and a push coalesce); a reconnect
// after a dropped stream reloads everything, since events were missed.
import { runOnce, state } from "./core.js";
import { fetchStatus, wtCount } from "./status.js";
import { reconcileStatusView } from "./files.js";
import { fetchBranches } from "./sidebar.js";
import { loadCommits, renderCommits } from "./commits.js";
import { loadRepo, refreshAfterOp } from "./ops.js";

const COALESCE_MS = 150; // one burst of watcher events → one refresh
const RETRY_MS = 500; // a refresh is already running → try again after it

const SIDEBAR = new Set(["branches", "remotes", "worktrees", "tags", "reflog"]);

const pending = new Set();
let timer = null;
let connected = false; // a second onopen is a RECONNECT → full refresh

function connectLive() {
  const es = new EventSource("/api/events");
  es.onmessage = (m) => {
    let msg;
    try {
      msg = JSON.parse(m.data);
    } catch {
      return;
    }
    if (msg.reason === "hello") {
      state.live = { enabled: !!msg.live, watch: !!msg.watch };
      if (connected) scheduleFull(); // reconnect: events were missed
      connected = true;
      return;
    }
    for (const src of msg.changed || []) pending.add(src);
    arm(COALESCE_MS);
  };
  // EventSource reconnects on its own; a re-hello then reloads in full.
  es.onerror = () => {};
  return es;
}

function arm(ms) {
  if (timer) return;
  timer = setTimeout(() => {
    timer = null;
    flush();
  }, ms);
}

function scheduleFull() {
  pending.add("status");
  pending.add("branches");
  pending.add("feed");
  arm(COALESCE_MS);
}

function flush() {
  if (!pending.size) return;
  if (state.op) {
    // An op owns the data: its refreshAfterOp reloads everything, so the
    // pending names are moot — drop them rather than replay stale ones.
    pending.clear();
    return;
  }
  const want = new Set(pending);
  pending.clear();
  const run = runOnce("refresh", () => refreshSources(want));
  if (!run) {
    // a manual `r` or an earlier flush is mid-flight — queue behind it
    for (const s of want) pending.add(s);
    arm(RETRY_MS);
    return;
  }
  run.catch(() => {});
}

// refreshSources reloads exactly what changed. It mirrors refreshAfterOp's
// cursor discipline: read the anchor BEFORE status (the working-tree row
// can appear or vanish and shift every index), reload the feed in
// RECONCILE mode (paged history and scroll survive), re-anchor after.
async function refreshSources(want) {
  const at = state.rows[state.cursor - wtCount()];
  const keep = at && at.hash;
  const jobs = [];
  if (want.has("status")) jobs.push(fetchStatus());
  let sidebar = false;
  for (const s of want) if (SIDEBAR.has(s)) sidebar = true;
  if (sidebar) jobs.push(fetchBranches(), loadRepo());
  await Promise.all(jobs);
  if (want.has("status")) reconcileStatusView();
  if (want.has("feed")) {
    await loadCommits(false, false);
    const last = state.rows.length + wtCount() - 1;
    const i = keep ? state.rows.findIndex((r) => r.hash === keep) : -1;
    if (i >= 0) state.cursor = i + wtCount();
    else if (state.cursor > last) state.cursor = Math.max(0, last);
  }
  renderCommits();
}

export { connectLive, refreshSources };
```

`refreshAfterOp` is imported for symmetry only if used — if the linter/reader objects to an unused import, drop it from the import line (the full reload path here is `scheduleFull`, not `refreshAfterOp`, to stay inside the `runOnce` gate).

- [ ] **Step 2: Boot it** — in `app.js` add `import { connectLive } from "./live.js";` after the `remoteheads.js` import, and in `boot()` after `focusPane();` add `connectLive();`.

- [ ] **Step 3: Browser verification** — build (`go build -o /mnt/t/others/gigagit/.claude/worktrees/web-live/gg ./cmd/gg`), write `.gg.toml` in a throwaway repo with `[refresh]\nenabled = true\nbranches_watch = true\nstatus = 10\n`, spawn `gg web`, open the page, wait for `.crow`, then from the probe script run `git commit --allow-empty -m "live one"` in the repo via `child_process.execFileSync`, and `page.waitForFunction(() => [...document.querySelectorAll(".crow .subj")].some(e => e.textContent.includes("live one")), null, { timeout: 5000 })` — no `r` press, no reload. Also `page.evaluate(() => state)` is not reachable (module scope); instead check `fetch("/api/events")` is open via `page.evaluate(() => performance.getEntriesByType("resource").some(r => r.name.endsWith("/api/events")))`. Screenshot `live-commit.png` and Read it. If the machine's checkout is on 9p (watch unsupported) the test repo must live under `/tmp` (ext4/tmpfs) — the scratchpad is.

- [ ] **Step 4: Go gates**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && go vet ./... && go test ./internal/web/ -count=1 2>&1 | tail -3`
Expected: pass.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-live && git add internal/web/static/live.js internal/web/static/app.js && git commit -m "feat(web): live.js subscribes to /api/events and re-fetches changed sources

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5"
```

---

### Task 8: docs + full gate

**Files:**
- Modify: `CHANGELOG.md` (top of Unreleased), `docs/web-tui-parity.md:32` and the "Weaker in the web" list, `README.md` (web section — `grep -n "gg web" README.md`).

- [ ] **Step 1: CHANGELOG** — insert as the first Unreleased bullet:

```markdown
- **`gg web` refreshes itself.** The web server now runs the same refresh
  machinery as the TUI: a file watcher on `.git` (branches, remotes, reflog,
  worktrees — `[refresh] <source>_watch`) and an interval lane for the rest
  (status, tags, the commit feed, plus the background `fetch` and
  `remote_tags` lookups), all driven by the per-repo `[refresh]` settings
  the TUI already honors. Changes are pushed to every open tab over one
  persistent `GET /api/events` stream and the page re-fetches only the
  sources that changed (the commit feed reconciles in place — paged history,
  scroll and cursor survive). Pushes pause while an operation started from
  the web is running; a dropped stream reconnects and reloads in full. The
  settings panel gained a file-watch toggle on each eligible interval row
  (and reports when the filesystem — WSL2 9p — cannot watch), and its refresh
  section no longer says "(TUI)": the same numbers now apply to both.
```

- [ ] **Step 2: parity doc** — in `docs/web-tui-parity.md` line 32's "Shared surface" list, "refresh settings" stays and add "live refresh (file watch + intervals)"; under "Weaker in the web" add:
```markdown
- **Live refresh.** A ref-family change (branches, remotes, worktrees, tags,
  reflog) reloads the whole sidebar in one go rather than the one list; the
  notification center still does not auto-refresh.
```

- [ ] **Step 3: README** — one sentence in the `gg web` paragraph: "The page refreshes itself on repo changes using the same `[refresh]` file-watch and interval settings as the TUI."

- [ ] **Step 4: Full gate**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/web-live && ./test.sh && ./test.sh race` (background it; ~25 min; poll).
Expected: exit 0, zero FAIL lines.

- [ ] **Step 5: Commit + deliver**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/web-live && git add CHANGELOG.md docs/web-tui-parity.md README.md && git commit -m "docs: web live refresh

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01P1xMWrgstP3PA2Jim8fyE5"
```
Then build a stamped verify binary at `/mnt/t/others/gigagit/.claude/worktrees/web-live/gg` and send it with SendUserFile. Do NOT merge — the user merges.
