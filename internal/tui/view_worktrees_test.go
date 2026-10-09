package tui

import (
	"strings"
	"testing"
)

// "* " follows the view: the worktree the panels show, whichever way it got
// there — a console, or the user's own switch.
func TestWorktreeRowsMarkTheViewedOne(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	rows := m.worktreeRows(m.worktreeEntries())
	for _, r := range rows {
		marked := strings.HasPrefix(r, "* ")
		if marked != strings.Contains(r, other) {
			t.Fatalf("rows = %q: only the viewed row carries * ", rows)
		}
	}
	m, _ = m.switchView(m.homeWorktree())
	for _, r := range m.worktreeRows(m.worktreeEntries()) {
		if strings.HasPrefix(r, "* ") != strings.Contains(r, m.homeWorktree()) {
			t.Fatalf("rows = %q: * must be back on home", rows)
		}
	}
}
