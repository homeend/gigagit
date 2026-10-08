package tui

import (
	"strings"
	"testing"
)

// Home keeps "* "; a viewed worktree that is not home gets "» ".
func TestWorktreeRowsMarkHomeAndTheViewedOne(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	rows := strings.Join(m.worktreeRows(m.worktreeEntries()), "\n")
	if !strings.Contains(rows, "* ") || !strings.Contains(rows, "» ") {
		t.Fatalf("rows = %q, want both markers", rows)
	}
	m, _ = m.switchView(m.home)
	if rows := strings.Join(m.worktreeRows(m.worktreeEntries()), "\n"); strings.Contains(rows, "» ") {
		t.Fatalf("rows = %q: the viewed marker must go once home is on screen", rows)
	}
}
