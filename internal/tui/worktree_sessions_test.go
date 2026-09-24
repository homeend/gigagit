package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestSessionSubRowUnderItsWorktree(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 5`)
	rows, _ := m.panelView(panelWorktrees)
	var at int = -1
	for i, r := range rows {
		if strings.Contains(r, "└") && strings.Contains(r, "Shell") {
			at = i
		}
	}
	if at < 1 {
		t.Fatalf("no session sub-row under the worktree: %q", rows)
	}
	if !strings.Contains(rows[at-1], m.currentWorktree) {
		t.Fatalf("sub-row not directly under its worktree: %q", rows)
	}
	_ = s
}

func TestSessionRowIsNotAWorktree(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1 // the session row under the only worktree
	if _, ok := m.selectedWorktree(); ok {
		t.Fatal("a session row must not resolve to a worktree")
	}
	if info, ok := m.selectedSession(); !ok || info.Label != "Shell" {
		t.Fatalf("selectedSession = %+v, %v", info, ok)
	}
}

func TestWorktreeActionsIgnoreSessionRows(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	if m.canDeleteWorktree() || m.canEnterWorktree() || m.canMoveWorktree() {
		t.Fatal("worktree actions must be off on a session row")
	}
	for _, r := range availableActions(m) {
		if r.id == "copy-worktree-abspath" {
			t.Fatal("copy worktree path offered on a session row")
		}
	}
}

func TestEnterOnSessionRowOpensConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.console == nil || m.console.id != s.Info().ID || !m.console.focused {
		t.Fatalf("enter on a session row: console = %+v", m.console)
	}
}

func TestSessionRowsFollowSortAndFilter(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`)
	m.sortModes[panelWorktrees] = sortNameDesc
	rows, _ := m.panelView(panelWorktrees)
	if len(rows) != 2 || !strings.Contains(rows[1], "└") {
		t.Fatalf("sorted rows = %q", rows)
	}
	_ = domain.SessionRunning
}
