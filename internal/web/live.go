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
