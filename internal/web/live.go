package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
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
	watchOK bool        // gitwatch.Supported(commonDir), cleared when the watcher failed to build
	gate    func() bool // true = an op is in flight → drop
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
		for _, p := range []struct {
			src string
			gs  gitwatch.Source
		}{{"worktrees", gitwatch.Worktrees}, {"reflog", gitwatch.Reflog}, {"branches", gitwatch.Branches}, {"remotes", gitwatch.Remotes}} {
			if h.watchActive(p.src) {
				enabled = append(enabled, p.gs)
			}
		}
	}
	if len(enabled) > 0 {
		worktreeDir, werr := svc.GitDir(ctx)
		if werr != nil || worktreeDir == "" {
			worktreeDir = common // main worktree: logs/HEAD lives at common
		}
		w, werr := gitwatch.New(gitwatch.Plan(common, worktreeDir, enabled), liveDebounce)
		h.mu.Lock()
		if werr == nil {
			h.watcher = w
		} else {
			// No watcher: flip watchOK so the ticker polls those sources
			// instead (a watch-active source has no interval backstop).
			h.watchOK = false
		}
		h.mu.Unlock()
		if werr == nil {
			go h.watchLoop(w)
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

// --- GET /api/events ----------------------------------------------------------

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
	var ch <-chan liveMsg // nil when no hub: select never fires, pings keep the stream open
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
