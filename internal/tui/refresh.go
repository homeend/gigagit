package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
)

// refreshItem is one schedulable background-refresh unit: a source read, or the
// synthetic fetch task (isFetch).
type refreshItem struct {
	source  sourceKey
	isFetch bool
}

var fetchItem = refreshItem{isFetch: true}

// scheduledItems is the fixed set the scheduler considers each tick: the panel
// sources plus fetch. (srcIdentity is intentionally excluded — identity changes
// only via the SetIdentity op, never on its own.)
var scheduledItems = []refreshItem{
	{source: srcStatus}, {source: srcBranches}, {source: srcRemotes},
	{source: srcWorktrees}, {source: srcTags}, {source: srcReflog},
	{source: srcFeed}, fetchItem,
}

// refreshIntervalFor returns the configured seconds for it (0 = off).
func refreshIntervalFor(cfg config.RefreshConfig, it refreshItem) int {
	if it.isFetch {
		return cfg.Fetch
	}
	switch it.source {
	case srcStatus:
		return cfg.Status
	case srcBranches:
		return cfg.Branches
	case srcRemotes:
		return cfg.Remotes
	case srcWorktrees:
		return cfg.Worktrees
	case srcTags:
		return cfg.Tags
	case srcReflog:
		return cfg.Reflog
	case srcFeed:
		return cfg.Feed
	}
	return 0
}

const (
	defaultMinSeconds  = 10 // floor on any auto-refresh interval (cheap sources don't hammer)
	maxDurationSamples = 10 // ring length per item for the rolling average
)

// meanDuration is the arithmetic mean of samples (0 for an empty slice).
func meanDuration(samples []time.Duration) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	var sum time.Duration
	for _, s := range samples {
		sum += s
	}
	return sum / time.Duration(len(samples))
}

// scheduledInterval returns an item's fixed poll interval in seconds and whether
// it is scheduled. The configured value is floored at min_seconds (default 10);
// a configured 0 means off. Measurements never affect this.
func scheduledInterval(cfg config.RefreshConfig, it refreshItem) (int, bool) {
	base := refreshIntervalFor(cfg, it)
	if base <= 0 {
		return 0, false
	}
	min := cfg.MinSeconds
	if min <= 0 {
		min = defaultMinSeconds
	}
	if base < min {
		base = min
	}
	return base, true
}

// refreshTomlKey is the [refresh] TOML key for an item. Note srcFeed's display
// name is "commits" but its config key is "feed".
func refreshTomlKey(it refreshItem) string {
	if it.isFetch {
		return "fetch"
	}
	switch it.source {
	case srcStatus:
		return "status"
	case srcBranches:
		return "branches"
	case srcRemotes:
		return "remotes"
	case srcWorktrees:
		return "worktrees"
	case srcTags:
		return "tags"
	case srcReflog:
		return "reflog"
	case srcFeed:
		return "feed"
	}
	return ""
}

// setRefreshIntervalField writes secs into the RefreshConfig field for an item.
func setRefreshIntervalField(cfg *config.RefreshConfig, it refreshItem, secs int) {
	if it.isFetch {
		cfg.Fetch = secs
		return
	}
	switch it.source {
	case srcStatus:
		cfg.Status = secs
	case srcBranches:
		cfg.Branches = secs
	case srcRemotes:
		cfg.Remotes = secs
	case srcWorktrees:
		cfg.Worktrees = secs
	case srcTags:
		cfg.Tags = secs
	case srcReflog:
		cfg.Reflog = secs
	case srcFeed:
		cfg.Feed = secs
	}
}

// refreshRateRows formats one line per scheduled item for the Refresh rates
// editor: name · interval · avg stat. Uses scheduledInterval for the interval
// (showing the floored value with a (min) marker when the configured value was
// below min_seconds). avg is informational only.
func (m Model) refreshRateRows() []string {
	rows := make([]string, 0, len(scheduledItems))
	for _, it := range scheduledItems {
		name := "fetch"
		if !it.isFetch {
			name = sourceNames[it.source]
		}
		cfgSecs := refreshIntervalFor(m.cfg.Refresh, it)
		secs, on := scheduledInterval(m.cfg.Refresh, it)
		intervalStr := "off"
		if on {
			intervalStr = fmt.Sprintf("every %ds", secs)
			if cfgSecs < secs {
				intervalStr += " (min)"
			}
		}
		samples := m.refreshDur[it]
		avgStr := "—"
		if len(samples) > 0 {
			avg := meanDuration(samples)
			if avg < time.Second {
				avgStr = fmt.Sprintf("%dms (%d)", avg.Milliseconds(), len(samples))
			} else {
				avgStr = fmt.Sprintf("%.1fs (%d)", avg.Seconds(), len(samples))
			}
		}
		rows = append(rows, fmt.Sprintf("%-10s  %-16s  avg %s", name, intervalStr, avgStr))
	}
	return rows
}

// recordDuration appends d to its rolling ring, dropping the oldest beyond
// maxDurationSamples. Lazy-inits the map so a literal-built test Model is safe.
func (m Model) recordDuration(it refreshItem, d time.Duration) Model {
	if m.refreshDur == nil {
		m.refreshDur = map[refreshItem][]time.Duration{}
	}
	ring := append(m.refreshDur[it], d)
	if len(ring) > maxDurationSamples {
		ring = ring[len(ring)-maxDurationSamples:]
	}
	m.refreshDur[it] = ring
	return m
}

// enqueueDue appends each due item that is neither already queued nor the
// currently-running item (when busy) — the dedup-by-type gate. FIFO order.
func enqueueDue(queue []refreshItem, active refreshItem, busy bool, due []refreshItem) []refreshItem {
	inQueue := func(it refreshItem) bool {
		for _, q := range queue {
			if q == it {
				return true
			}
		}
		return false
	}
	for _, d := range due {
		if busy && d == active {
			continue
		}
		if inQueue(d) {
			continue
		}
		queue = append(queue, d)
	}
	return queue
}

// refreshSuppressed reports whether background auto-refresh must hold off right
// now: a running op, an open overlay/modal/decider, or active filter/search
// typing. (Per-source in-flight is handled per item in refreshTick.)
func (m Model) refreshSuppressed() bool {
	if m.running || m.loading {
		return true
	}
	if m.modal != nil || m.topLayer() != nil {
		return true
	}
	if m.filterTyping || m.highlightTyping {
		return true
	}
	return false
}

// refreshTick is called from the heartbeat. It enqueues newly-due items (deduped
// by type) and, when the single background lane is free, drains exactly one read
// under a shared cancellable bg context that a starting user op cancels.
func (m Model) refreshTick(now time.Time) (Model, tea.Cmd) {
	if m.refreshSuppressed() {
		return m, nil
	}
	due := dueItems(now, m.refreshLastRun, m.cfg.Refresh, false)
	m.bgQueue = enqueueDue(m.bgQueue, m.bgActiveItem, m.bgBusy, due)
	if m.bgBusy || len(m.bgQueue) == 0 {
		return m, nil
	}
	it := m.bgQueue[0]
	m.bgQueue = m.bgQueue[1:]
	// A source whose read is already in flight (e.g. a manual r) must not get a
	// second, superseding background read — that would strand the manual ⏳.
	// Drop it this tick; it re-enqueues next tick if still due (lastRun unchanged).
	if !it.isFetch && m.srcInflight[it.source] {
		return m, nil
	}
	// Guard fetch when svc is nil: bgFetchCmd would return nil (no command), but
	// the lane would already be committed (bgBusy=true, lastRun stamped), stranding
	// it permanently. Drop it this tick rather than committing an unrunnable fetch.
	if it.isFetch && m.svc == nil {
		return m, nil
	}
	if m.bgCancel == nil {
		m.bgCtx, m.bgCancel = context.WithCancel(context.Background())
	}
	m.bgBusy = true
	m.bgActiveItem = it
	m.refreshLastRun[it] = now
	if it.isFetch {
		return m, m.bgFetchCmd(m.bgCtx)
	}
	m.srcGen[it.source]++
	m.srcInflight[it.source] = true
	return m, m.readSourceCmd(m.bgCtx, it.source, false, false) // manual=false → silent; startup=false → measured
}

// bgFetchDoneMsg lands when a background fetch finishes. On success the handler
// fires a silent remotes refresh; on error it is swallowed (already recorded to
// the session error log by the domain failure seam).
type bgFetchDoneMsg struct {
	dur time.Duration
	err error
}

// bgFetchCmd runs `git fetch` quietly — outside the foreground op slot, no
// m.running, no modal — under the background context. Events are discarded;
// fetch does not fork so an empty MapDecider (which errors on any unexpected
// decision) is the correct belt-and-braces decider.
func (m Model) bgFetchCmd(ctx context.Context) tea.Cmd {
	if m.svc == nil {
		return nil
	}
	svc := m.svc
	return func() tea.Msg {
		start := time.Now()
		events := make(chan engine.Event, 8)
		go func() {
			for range events {
			}
		}()
		_, err := svc.Execute(ctx, engine.Fetch{}, events, engine.MapDecider{})
		close(events)
		return bgFetchDoneMsg{dur: time.Since(start), err: err}
	}
}

// dueItems returns the items whose fixed interval has elapsed this tick. off
// items are excluded. Pure.
func dueItems(now time.Time, lastRun map[refreshItem]time.Time, cfg config.RefreshConfig, suppressed bool) []refreshItem {
	if !cfg.Enabled || suppressed {
		return nil
	}
	var due []refreshItem
	for _, it := range scheduledItems {
		secs, on := scheduledInterval(cfg, it)
		if !on {
			continue
		}
		last, seen := lastRun[it]
		if !seen || now.Sub(last) >= time.Duration(secs)*time.Second {
			due = append(due, it)
		}
	}
	return due
}

// bgRefreshHint is the unobtrusive status-line marker shown while the single
// background read runs (active-only, no countdown). Empty when the lane is idle.
// It is also suppressed for known-fast reads (rolling average < 1s) so quick
// sources don't flicker the status bar — the hint only appears when a read is
// actually taking a moment. A not-yet-measured read shows the hint (we can't
// know it's fast, and a slow first read is exactly when the hint helps).
func (m Model) bgRefreshHint() string {
	if !m.bgBusy {
		return ""
	}
	if samples := m.refreshDur[m.bgActiveItem]; len(samples) > 0 && meanDuration(samples) < time.Second {
		return ""
	}
	name := "fetch"
	if !m.bgActiveItem.isFetch {
		name = sourceNames[m.bgActiveItem.source]
	}
	return "⟳ " + name + "…"
}
