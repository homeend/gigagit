package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func TestMeanDuration(t *testing.T) {
	if got := meanDuration(nil); got != 0 {
		t.Fatalf("empty → 0, got %v", got)
	}
	got := meanDuration([]time.Duration{2 * time.Second, 4 * time.Second})
	if got != 3*time.Second {
		t.Fatalf("mean(2s,4s) = 3s, got %v", got)
	}
}

func TestEffectiveInterval(t *testing.T) {
	st := refreshItem{source: srcStatus}
	base := config.RefreshConfig{Enabled: true, Status: 10} // adaptive on (DisableAdaptive false)

	// cfg interval 0 → off.
	if secs, state := effectiveInterval(config.RefreshConfig{Enabled: true}, st, 0, false); state != stateOff || secs != 0 {
		t.Fatalf("interval 0 → off, got %d/%v", secs, state)
	}
	// adaptive off → fixed at configured.
	fixed := base
	fixed.DisableAdaptive = true
	if secs, state := effectiveInterval(fixed, st, 4*time.Second, true); state != stateFixed || secs != 10 {
		t.Fatalf("adaptive off → fixed 10, got %d/%v", secs, state)
	}
	// adaptive on, no sample → floor (run at configured pending measurement).
	if secs, state := effectiveInterval(base, st, 0, false); state != stateAdaptiveFloor || secs != 10 {
		t.Fatalf("no sample → floor 10, got %d/%v", secs, state)
	}
	// adaptive on, cheap read (0.3s) → floor wins (max(10, ceil(3s))=10).
	if secs, state := effectiveInterval(base, st, 300*time.Millisecond, true); state != stateAdaptiveFloor || secs != 10 {
		t.Fatalf("cheap → floor 10, got %d/%v", secs, state)
	}
	// adaptive on, 4.1s read → backoff 41 (max(10, ceil(41.0))).
	if secs, state := effectiveInterval(base, st, 4100*time.Millisecond, true); state != stateAdaptive || secs != 41 {
		t.Fatalf("4.1s → adaptive 41, got %d/%v", secs, state)
	}
	// adaptive on, 12s read > cutoff(10) → disabled.
	if secs, state := effectiveInterval(base, st, 12*time.Second, true); state != stateDisabled || secs != 0 {
		t.Fatalf("12s → disabled, got %d/%v", secs, state)
	}
	// custom cutoff/factor honored.
	custom := config.RefreshConfig{Enabled: true, Status: 5, MaxReadSeconds: 20, BackoffFactor: 3}
	if secs, state := effectiveInterval(custom, st, 12*time.Second, true); state != stateAdaptive || secs != 36 {
		t.Fatalf("custom factor 3×12s=36, got %d/%v", secs, state)
	}
}

func TestRecordDurationCapsAtTen(t *testing.T) {
	m := Model{refreshDur: map[refreshItem][]time.Duration{}}
	it := refreshItem{source: srcStatus}
	for i := 1; i <= 13; i++ {
		m = m.recordDuration(it, time.Duration(i)*time.Second)
	}
	got := m.refreshDur[it]
	if len(got) != maxDurationSamples {
		t.Fatalf("ring should cap at %d, got %d", maxDurationSamples, len(got))
	}
	// Oldest dropped: the ring holds samples 4s..13s, mean = 8.5s.
	if mean := meanDuration(got); mean != 8500*time.Millisecond {
		t.Fatalf("want mean 8.5s of last 10, got %v", mean)
	}
}

func TestDataAvailableRecordsDuration(t *testing.T) {
	m := newTestModel(t)
	m.bgActiveItem = refreshItem{} // lane idle
	it := refreshItem{source: srcTags}
	gen := m.srcGen[srcTags]
	msg := dataAvailableMsg{source: srcTags, gen: gen, value: []model.Tag(nil), dur: 2 * time.Second}
	nm, _ := m.Update(msg)
	got := nm.(Model).refreshDur[it]
	if len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("status handler should record dur, got %v", got)
	}
}

func TestEnqueueDueDedup(t *testing.T) {
	a := refreshItem{source: srcStatus}
	b := refreshItem{source: srcBranches}

	// Empty queue, not busy → both appended in FIFO order.
	q := enqueueDue(nil, refreshItem{}, false, []refreshItem{a, b})
	if len(q) != 2 || q[0] != a || q[1] != b {
		t.Fatalf("want [a b], got %v", q)
	}
	// Re-enqueue same types → no duplicates.
	q = enqueueDue(q, refreshItem{}, false, []refreshItem{a, b})
	if len(q) != 2 {
		t.Fatalf("dedup failed, got %v", q)
	}
	// Active (busy) type is not re-enqueued even though absent from the queue.
	q2 := enqueueDue(nil, a, true, []refreshItem{a, b})
	if len(q2) != 1 || q2[0] != b {
		t.Fatalf("active type must be skipped, got %v", q2)
	}
}

func TestRefreshTickSingleLane(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 1, Branches: 1}
	m.loading = false
	t0 := time.Unix(4_000_000, 0)
	// First tick: both due, but only ONE fires (lane single-file); the other queues.
	m2, cmd := m.refreshTick(t0)
	if cmd == nil || !m2.bgBusy {
		t.Fatal("first tick should fire one read and mark the lane busy")
	}
	if len(m2.bgQueue) != 1 {
		t.Fatalf("the second due item should be queued, got %v", m2.bgQueue)
	}
	// Second tick while busy: nothing new fires.
	_, cmd2 := m2.refreshTick(t0)
	if cmd2 != nil {
		t.Fatal("must not fire a second read while the lane is busy")
	}
}

func TestManualRFreesLane(t *testing.T) {
	// Reproduces the stranding bug: a bg read of branches is in flight; manual r
	// bumps the gen; the bg message arrives stale and MUST still free the lane.
	m := newTestModel(t)
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcBranches}
	staleGen := m.srcGen[srcBranches]
	m.srcGen[srcBranches] = staleGen + 1 // simulate manual r having superseded it
	msg := dataAvailableMsg{source: srcBranches, gen: staleGen, manual: false, value: []model.Branch(nil)}
	nm, _ := m.Update(msg)
	if nm.(Model).bgBusy {
		t.Fatal("a stale bg read message must still free the lane (stranding bug)")
	}
}

func TestStartOpClearsLaneAndQueue(t *testing.T) {
	m := newTestModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	m.bgCtx, m.bgCancel = ctx, cancel
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcTags}
	m.bgQueue = []refreshItem{{source: srcStatus}}
	m2, _ := m.startOp(engine.Fetch{})
	if m2.bgBusy || len(m2.bgQueue) != 0 || m2.bgCancel != nil {
		t.Fatalf("startOp must clear the lane + queue, got busy=%v queue=%v", m2.bgBusy, m2.bgQueue)
	}
}

func TestRefreshRateRows(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 10, Remotes: 30}
	// status has samples averaging 4s → adaptive 40s; remotes none → floor.
	it := refreshItem{source: srcStatus}
	m.refreshDur[it] = []time.Duration{4 * time.Second, 4 * time.Second}
	rows := m.refreshRateRows()
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "status") || !strings.Contains(joined, "40s") {
		t.Fatalf("status row should show adaptive 40s, got:\n%s", joined)
	}
	if !strings.Contains(joined, "remotes") {
		t.Fatalf("remotes row missing, got:\n%s", joined)
	}
}

func TestBgRefreshHint(t *testing.T) {
	m := newTestModel(t)
	if m.bgRefreshHint() != "" {
		t.Fatal("idle lane → no hint")
	}
	m.bgBusy = true
	m.bgActiveItem = refreshItem{source: srcBranches}
	if got := m.bgRefreshHint(); got != "⟳ branches…" {
		t.Fatalf("want '⟳ branches…', got %q", got)
	}
	m.bgActiveItem = fetchItem
	if got := m.bgRefreshHint(); got != "⟳ fetch…" {
		t.Fatalf("want '⟳ fetch…', got %q", got)
	}
}
