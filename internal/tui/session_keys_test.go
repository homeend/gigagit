package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// alt+A walks the agents of the VIEWED worktree only, wrapping; an agent
// in another worktree is never a stop.
func TestAltShiftAWalksTheViewedWorktreesAgents(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	a1 := startSessionIn(t, m, home, "A1")
	a2 := startSessionIn(t, m, home, "A2")
	startSessionIn(t, m, other, "B1")
	m = pressAlt(t, m, 'A')
	if m.console == nil || m.console.id != a1 || !m.console.focused || m.viewed != model.KeyOf(home) {
		t.Fatalf("first: console=%+v viewed=%q, want A1 bound at home", m.console, m.viewed)
	}
	m = pressAlt(t, m, 'A')
	if m.console == nil || m.console.id != a2 {
		t.Fatalf("second: console=%+v, want A2", m.console)
	}
	m = pressAlt(t, m, 'A')
	if m.console == nil || m.console.id != a1 || m.viewed != model.KeyOf(home) {
		t.Fatalf("third: console=%+v viewed=%q, want A1 again (never B1)", m.console, m.viewed)
	}
}

// alt+A with no agent in the viewed worktree does nothing, silently.
func TestAltShiftAWithNoAgentHereDoesNothing(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	startSessionIn(t, m, other, "B1")
	m = pressAlt(t, m, 'A')
	if m.console != nil || m.viewed != m.home || m.statusMsg != "" {
		t.Fatalf("console=%v viewed=%q status=%q: alt+A acted with no agent here", m.console != nil, m.viewed, m.statusMsg)
	}
}

// alt+T is the same for terminals; agents are not its stops.
func TestAltShiftTWalksTheViewedWorktreesTerminals(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	home := m.currentWorktree
	installSessionManager(t)
	startSessionIn(t, m, home, "A1")
	t1 := startTerminalIn(t, m, home, "T1")
	m = pressAlt(t, m, 'T')
	if m.console == nil || m.console.id != t1 || !m.console.focused {
		t.Fatalf("console=%+v, want the terminal T1 bound", m.console)
	}
	m = pressAlt(t, m, 'T')
	if m.console == nil || m.console.id != t1 {
		t.Fatalf("console=%+v, want T1 again (the only terminal here)", m.console)
	}
}

// startTerminalIn starts a real (sleeping) terminal session in dir.
func startTerminalIn(t *testing.T, m Model, dir, name string) domain.SessionID {
	t.Helper()
	s, err := m.svc.StartTerminal(t.Context(), "sh", dir, "", 80, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = name
	return s.Info().ID
}
