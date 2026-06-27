package tui

import (
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
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
