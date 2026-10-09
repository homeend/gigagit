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

// alt+f on a focused session toggles docked ↔ maximized; after the toggle
// the session is bound and focused. From an unbound focused console it
// binds too. From another panel it does nothing.
func TestAltFTogglesTheConsoleSizeAndBinds(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	id := startSessionIn(t, m, m.currentWorktree, "A1")
	m, _ = m.showConsole(id, true) // docked, bound
	if m.console == nil || m.console.maximized {
		t.Fatalf("precondition: %+v", m.console)
	}
	m = pressAlt(t, m, 'f')
	if m.console == nil || !m.console.maximized || !m.console.focused || m.focus != panelCommits {
		t.Fatalf("after alt+f: %+v focus=%v, want maximized, bound, focused", m.console, m.focus)
	}
	m = pressAlt(t, m, 'f')
	if m.console == nil || m.console.maximized || !m.console.focused {
		t.Fatalf("after the second alt+f: %+v, want docked and still bound", m.console)
	}
	m.console.focused = false // unbound, still focused (the blue border)
	m = pressAlt(t, m, 'f')
	if m.console == nil || !m.console.maximized || !m.console.focused {
		t.Fatalf("alt+f on an unbound focused console: %+v, want maximized and bound", m.console)
	}
	m.console.focused = false
	m.focus = panelFiles // another panel has the keyboard: not focused
	m = pressAlt(t, m, 'f')
	if m.console == nil || !m.console.maximized || m.console.focused {
		t.Fatalf("alt+f from another panel changed the console: %+v", m.console)
	}
}

// alt+b on a focused session toggles bound: bound → unbound (shown,
// focused), unbound → bound. From another panel it does nothing.
func TestAltBTogglesTheConsoleBinding(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	id := startSessionIn(t, m, m.currentWorktree, "A1")
	m, _ = m.showConsole(id, true)
	m = pressAlt(t, m, 'b')
	if m.console == nil || m.console.focused || m.focus != panelCommits {
		t.Fatalf("after alt+b: %+v focus=%v, want unbound, shown, focused", m.console, m.focus)
	}
	m = pressAlt(t, m, 'b')
	if m.console == nil || !m.console.focused {
		t.Fatalf("after the second alt+b: %+v, want bound", m.console)
	}
	m.console.focused = false
	m.focus = panelFiles
	m = pressAlt(t, m, 'b')
	if m.console == nil || m.console.focused {
		t.Fatalf("alt+b from another panel bound the console: %+v", m.console)
	}
}

// The session walks put the Branches cursor on the shown session's row
// (its sub-row under the branch of its worktree), the Branches tab active,
// while the console keeps the keyboard.
func TestSessionWalksSelectTheSessionsBranchesRow(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	a1 := startSessionIn(t, m, home, "A1")
	b1 := startSessionIn(t, m, other, "B1")
	m.activeLeftTab, m.focus = panelWorktrees, panelFiles
	for _, want := range []domain.SessionID{a1, b1, a1} {
		m = pressAlt(t, m, 'a')
		if m.console == nil || m.console.id != want {
			t.Fatalf("console=%+v, want %s", m.console, want)
		}
		if m.activeLeftTab != panelBranches || m.focus != panelCommits {
			t.Fatalf("tab=%v focus=%v, want the Branches tab shown and the console keeping the keyboard", m.activeLeftTab, m.focus)
		}
		e, ok := m.selectedBranchEntry()
		if !ok || e.sess != want {
			t.Fatalf("Branches cursor on %+v, want the row of %s", e, want)
		}
	}
	m = pressAlt(t, m, 'A') // the scoped walk too
	if e, ok := m.selectedBranchEntry(); !ok || e.sess != a1 {
		t.Fatalf("after alt+A: Branches cursor on %+v, want A1's row", e)
	}
}
