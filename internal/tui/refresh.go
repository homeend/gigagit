package tui

import (
	"context"
	"fmt"
	"math"
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
	defaultMaxReadSeconds = 10
	defaultBackoffFactor  = 10
	maxDurationSamples    = 10 // ring length per item for the rolling average
)

// intervalState classifies how an item's effective interval was derived (for the
// "Refresh rates" viewer and the scheduler's skip decision).
type intervalState int

const (
	stateOff           intervalState = iota // adaptive off + configured interval 0 → never auto-refresh
	stateFixed                              // adaptive off → configured interval verbatim
	stateAdaptive                           // backoff_factor × avg drives the interval
	stateAdaptiveFloor                      // configured floor won (cheap read), or floor used pending measurement
	statePending                            // adaptive on, no configured floor, not yet measured → waits for first read
	stateDisabled                           // avg > cutoff → auto-refresh disabled (manual only)
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

// effectiveInterval returns an item's effective interval in seconds and the
// state that produced it. secs is meaningful only for the fixed/adaptive states;
// stateOff, statePending, and stateDisabled return 0 (the item does not
// auto-refresh this tick).
//
// The configured interval (base) is an OPTIONAL FLOOR. With adaptive on, a
// source's interval is driven by its measured average (backoff_factor × avg);
// when no interval is configured (base 0) the source still auto-refreshes once
// it has a measurement, polling purely at backoff_factor × avg. Before any
// measurement, a base-0 source is statePending (it starts once a manual r or
// background read measures it). With adaptive off, the configured interval is
// used verbatim and base 0 means off.
func effectiveInterval(cfg config.RefreshConfig, it refreshItem, avg time.Duration, haveSample bool) (int, intervalState) {
	base := refreshIntervalFor(cfg, it)
	// The background `git fetch` is network I/O, so it is purely opt-in: it runs
	// only when [refresh] fetch is set (base > 0). Unlike local source reads it
	// never floor-less auto-starts from a measurement and is never "pending". A
	// foreground fetch may record a sample (for visibility), but base 0 still
	// means off — the guard returns before haveSample is consulted.
	if it.isFetch && base <= 0 {
		return 0, stateOff
	}
	if cfg.DisableAdaptive {
		if base <= 0 {
			return 0, stateOff
		}
		return base, stateFixed
	}
	// adaptive on
	if !haveSample {
		if base > 0 {
			return base, stateAdaptiveFloor // poll at the configured floor until measured
		}
		return 0, statePending // no floor, no data yet → wait for the first read
	}
	cutoff := cfg.MaxReadSeconds
	if cutoff <= 0 {
		cutoff = defaultMaxReadSeconds
	}
	if avg > time.Duration(cutoff)*time.Second {
		return 0, stateDisabled
	}
	factor := cfg.BackoffFactor
	if factor <= 0 {
		factor = defaultBackoffFactor
	}
	backoff := int(math.Ceil((time.Duration(factor) * avg).Seconds()))
	if backoff <= base {
		return base, stateAdaptiveFloor // configured floor wins
	}
	return backoff, stateAdaptive // base 0 → polls purely at backoff_factor × avg
}

// stateLabel renders an intervalState for the Refresh rates viewer.
func stateLabel(s intervalState) string {
	switch s {
	case stateOff:
		return "off"
	case stateFixed:
		return "fixed"
	case stateAdaptive:
		return "adaptive"
	case stateAdaptiveFloor:
		return "adaptive (floor)"
	case statePending:
		return "pending — press r to measure"
	case stateDisabled:
		return "disabled (too slow)"
	}
	return ""
}

// refreshRateRows formats one line per scheduled item for the Settings viewer:
// name · configured · avg (n) · effective · state.
func (m Model) refreshRateRows() []string {
	rows := make([]string, 0, len(scheduledItems))
	for _, it := range scheduledItems {
		name := "fetch"
		if !it.isFetch {
			name = sourceNames[it.source]
		}
		samples := m.refreshDur[it]
		avg := meanDuration(samples)
		secs, state := effectiveInterval(m.cfg.Refresh, it, avg, len(samples) > 0)
		cfgSecs := refreshIntervalFor(m.cfg.Refresh, it)
		cfgStr := "off"
		if cfgSecs > 0 {
			cfgStr = fmt.Sprintf("%ds", cfgSecs)
		}
		avgStr := "—"
		if len(samples) > 0 {
			avgStr = fmt.Sprintf("%.1fs (%d)", avg.Seconds(), len(samples))
		}
		effStr := "—"
		if state == stateFixed || state == stateAdaptive || state == stateAdaptiveFloor {
			effStr = fmt.Sprintf("%ds", secs)
		}
		rows = append(rows, fmt.Sprintf("%-10s  cfg %-5s  avg %-12s  eff %-5s  %s", name, cfgStr, avgStr, effStr, stateLabel(state)))
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
	due := dueItems(now, m.refreshLastRun, m.refreshDur, m.cfg.Refresh, false)
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

// dueItems returns the items whose effective interval has elapsed this tick.
// off/disabled/pending items (effective interval <= 0) are excluded. Pure: durs
// is the per-item duration ring.
func dueItems(now time.Time, lastRun map[refreshItem]time.Time, durs map[refreshItem][]time.Duration, cfg config.RefreshConfig, suppressed bool) []refreshItem {
	if !cfg.Enabled || suppressed {
		return nil
	}
	var due []refreshItem
	for _, it := range scheduledItems {
		avg := meanDuration(durs[it])
		secs, _ := effectiveInterval(cfg, it, avg, len(durs[it]) > 0)
		if secs <= 0 {
			continue // off / disabled / pending → not scheduled this tick
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
func (m Model) bgRefreshHint() string {
	if !m.bgBusy {
		return ""
	}
	name := "fetch"
	if !m.bgActiveItem.isFetch {
		name = sourceNames[m.bgActiveItem.source]
	}
	return "⟳ " + name + "…"
}
