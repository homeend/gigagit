package tui

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
)

func TestDueItemsRespectsIntervalAndMaster(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cfg := config.RefreshConfig{Enabled: true, Status: 30, Branches: 0}
	last := map[refreshItem]time.Time{{source: srcStatus}: t0}

	// 29s later: not due.
	if d := dueItems(t0.Add(29*time.Second), last, cfg, false); len(d) != 0 {
		t.Fatalf("status should not be due at 29s, got %v", d)
	}
	// 31s later: due.
	d := dueItems(t0.Add(31*time.Second), last, cfg, false)
	if len(d) != 1 || d[0].source != srcStatus {
		t.Fatalf("status should be due at 31s, got %v", d)
	}
	// Branches interval 0 → never due even at 31s.
	for _, it := range d {
		if it.source == srcBranches {
			t.Fatal("branches (interval 0) must never be due")
		}
	}
	// Master off → nothing due.
	if d := dueItems(t0.Add(31*time.Second), last, cfg, false); len(d) == 1 {
		cfgOff := cfg
		cfgOff.Enabled = false
		if d2 := dueItems(t0.Add(31*time.Second), last, cfgOff, false); len(d2) != 0 {
			t.Fatalf("master off must yield nothing, got %v", d2)
		}
	}
	// Suppressed → nothing due.
	if d := dueItems(t0.Add(31*time.Second), last, cfg, true); len(d) != 0 {
		t.Fatalf("suppressed must yield nothing, got %v", d)
	}
}

func TestDueItemsFirstRunWhenUnseen(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cfg := config.RefreshConfig{Enabled: true, Status: 30}
	// No lastRun entry → treat as due immediately (first poll after enable).
	d := dueItems(t0, map[refreshItem]time.Time{}, cfg, false)
	if len(d) != 1 || d[0].source != srcStatus {
		t.Fatalf("unseen item with interval>0 should be due, got %v", d)
	}
}

func TestRefreshTickFiresSilentReadAndIsSuppressed(t *testing.T) {
	m := newTestModel(t)
	m.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 30}
	m.loading = false // simulate post-init: New() starts with loading=true; clear it here
	t0 := time.Unix(2_000_000, 0)

	// Not suppressed, status unseen → due → fires a silent read (gen bumps, NO srcLoading).
	genBefore := m.srcGen[srcStatus]
	m2, cmd := m.refreshTick(t0)
	if cmd == nil {
		t.Fatal("expected a background read command")
	}
	if m2.srcGen[srcStatus] != genBefore+1 {
		t.Fatal("status gen should bump for a fired background read")
	}
	if m2.srcLoading[srcStatus] {
		t.Fatal("background (auto) read must NOT set srcLoading (silent)")
	}

	// Suppressed (op running) → no fire.
	mb := newTestModel(t)
	mb.cfg.Refresh = config.RefreshConfig{Enabled: true, Status: 30}
	mb.loading = false // same post-init baseline
	mb.running = true
	_, cmd2 := mb.refreshTick(t0)
	if cmd2 != nil {
		t.Fatal("must not fire while an op is running")
	}
}

// BLOCKING-bug guard: a silent (auto) read that fails — e.g. context.Canceled
// because a user op preempted it — must NEVER write the status line. Otherwise
// the user sees "branches: context canceled" from a refresh they never asked
// for. (Phase A's handler currently surfaces every err to statusMsg.)
func TestSilentReadErrorDoesNotTouchStatus(t *testing.T) {
	m := newTestModel(t)
	m.statusMsg = "keep me"
	nm, _ := m.Update(dataAvailableMsg{
		source: srcBranches, gen: m.srcGen[srcBranches],
		manual: false, err: context.Canceled,
	})
	if got := nm.(Model).statusMsg; got != "keep me" {
		t.Fatalf("silent read error must not change statusMsg, got %q", got)
	}
}
