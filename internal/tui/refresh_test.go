package tui

import (
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
