package tui

import (
	"context"
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
	stateOff           intervalState = iota // configured interval 0 → never auto-refresh
	stateFixed                              // adaptive off → configured interval verbatim
	stateAdaptive                           // backoff_factor × avg won (interval lengthened)
	stateAdaptiveFloor                      // configured floor won (cheap read, or not yet measured)
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
// stateOff and stateDisabled return 0 (the item does not auto-refresh).
func effectiveInterval(cfg config.RefreshConfig, it refreshItem, avg time.Duration, haveSample bool) (int, intervalState) {
	base := refreshIntervalFor(cfg, it)
	if base <= 0 {
		return 0, stateOff
	}
	if cfg.DisableAdaptive {
		return base, stateFixed
	}
	if !haveSample {
		return base, stateAdaptiveFloor // adaptive on, not yet measured → run at floor
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
		return base, stateAdaptiveFloor
	}
	return backoff, stateAdaptive
}

// recordDuration appends d to it's rolling ring, dropping the oldest beyond
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

// refreshTick is called from the heartbeat. It fires silent reads for every due
// item that is not already in-flight, under a shared cancellable bg context that
// a starting user op cancels.
func (m Model) refreshTick(now time.Time) (Model, tea.Cmd) {
	due := dueItems(now, m.refreshLastRun, m.cfg.Refresh, m.refreshSuppressed())
	if len(due) == 0 {
		return m, nil
	}
	if m.bgCancel == nil {
		m.bgCtx, m.bgCancel = context.WithCancel(context.Background())
	}
	var cmds []tea.Cmd
	for _, it := range due {
		if it.isFetch {
			m.refreshLastRun[it] = now
			if cmd := m.bgFetchCmd(m.bgCtx); cmd != nil {
				cmds = append(cmds, cmd)
			}
			continue
		}
		if m.srcInflight[it.source] {
			continue // don't stack a second read of a source already loading
		}
		m.srcGen[it.source]++
		m.srcInflight[it.source] = true
		m.refreshLastRun[it] = now
		cmds = append(cmds, m.readSourceCmd(m.bgCtx, it.source, false)) // manual=false → silent
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
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

// dueItems returns the items that should fire now: master enabled, not
// suppressed, interval > 0, and (now - lastRun) >= interval. An item with no
// lastRun entry is due immediately (first poll after enabling).
func dueItems(now time.Time, lastRun map[refreshItem]time.Time, cfg config.RefreshConfig, suppressed bool) []refreshItem {
	if !cfg.Enabled || suppressed {
		return nil
	}
	var due []refreshItem
	for _, it := range scheduledItems {
		secs := refreshIntervalFor(cfg, it)
		if secs <= 0 {
			continue
		}
		last, seen := lastRun[it]
		if !seen || now.Sub(last) >= time.Duration(secs)*time.Second {
			due = append(due, it)
		}
	}
	return due
}
