package tui

import (
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"

	"github.com/homeend/gigagit/internal/domain"
)

// worktreeIndex is a worktree's position in the Worktrees list.
func worktreeIndex(t *testing.T, m Model, dir string) int {
	t.Helper()
	for i, w := range m.worktrees {
		if filepath.Clean(w.Path) == filepath.Clean(dir) {
			return i
		}
	}
	t.Fatalf("%q is not a listed worktree", dir)
	return -1
}

// alt+a walks the agents in the Worktrees list's order, binds the one it
// shows, and never leaves for the starting screen: round and round.
func TestAltAWalksAgentsInWorktreeOrderAndNeverReturns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, wtA := addWorktree(t, m, "wtA")
	m, wtB := addWorktree(t, m, "wtB")
	installSessionManager(t)
	idB := startSessionIn(t, m, wtB, "B") // started first: last use must not matter
	idA := startSessionIn(t, m, wtA, "A")
	first, second := idA, idB
	if worktreeIndex(t, m, wtB) < worktreeIndex(t, m, wtA) {
		first, second = idB, idA
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != first || !m.console.focused || m.focus != panelCommits {
		t.Fatalf("first alt+a: console=%+v focus=%v, want %s bound", m.console, m.focus, first)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != second || !m.console.focused {
		t.Fatalf("second alt+a: console=%+v, want %s bound", m.console, second)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != first || !m.console.focused {
		t.Fatalf("third alt+a: console=%+v, want %s again — no return stop", m.console, first)
	}
}

// With gg's keyboard, the viewed worktree's own session comes first: a
// hidden one is shown and bound, a shown unbound one is bound.
func TestAltABindsTheViewedWorktreesOwnSessionFirst(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	startSessionIn(t, m, other, "Other")
	home := startSessionIn(t, m, m.homeWorktree(), "Home")
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != home || !m.console.focused {
		t.Fatalf("console=%+v, want the viewed worktree's own session bound", m.console)
	}
	mm, _ := m.Update(ctrlBracket()) // unbind: gg's keyboard, the console still shown
	m = mm.(Model)
	if m.console.focused {
		t.Fatal("precondition: step-out must unbind")
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != home || !m.console.focused {
		t.Fatalf("console=%+v, want the shown console bound, not the next one", m.console)
	}
}

// No session in the viewed worktree: the nearest one below in list order,
// wrapping past the end.
func TestAltAStartsAtTheViewedWorktreeAndWraps(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, wtA := addWorktree(t, m, "wtA")
	m, wtB := addWorktree(t, m, "wtB")
	installSessionManager(t)
	idA := startSessionIn(t, m, wtA, "A")
	last := wtB
	if worktreeIndex(t, m, wtA) > worktreeIndex(t, m, wtB) {
		last = wtA
	}
	if last == wtA {
		t.Skip("list order puts wtA last; the wrap needs a session-less worktree after it")
	}
	m, ok := m.switchView(wtB) // viewed: the last worktree, no session
	if !ok {
		t.Fatal("switchView refused")
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != idA || m.viewed != model.KeyOf(wtA) {
		t.Fatalf("console=%+v viewed=%q, want A (wrapped from the end)", m.console, m.viewed)
	}
}

// One agent, bound: alt+a does nothing but say so.
func TestAltAWithTheOnlyAgentBoundStays(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	s := startTestSession(t, m, `sleep 5`)
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.focused {
		t.Fatalf("console=%+v", m.console)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != s.Info().ID || !m.console.focused {
		t.Fatalf("console=%+v, want the same bound console", m.console)
	}
	if m.statusMsg == "" {
		t.Fatal("the status line must say there is only one")
	}
}

// Two agents in one worktree go by start time, oldest first.
func TestAltAOrdersSessionsOfOneWorktreeByStart(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	a := startTestSession(t, m, `sleep 5`)
	b := startSecondSession(t, a, "b", false)
	a.Touch() // last use must not matter
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != a.Info().ID {
		t.Fatalf("console=%+v, want a (older)", m.console)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != b.Info().ID {
		t.Fatalf("console=%+v, want b", m.console)
	}
}

// alt+t over a bound agent goes to the terminal ring from the viewed
// worktree; alt+t with no terminal says so and keeps the agent.
func TestAltTFromABoundAgentWalksTerminals(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	a := startTestSession(t, m, `sleep 5`)
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.focused {
		t.Fatalf("console=%+v", m.console)
	}
	m = pressAlt(t, m, 't')
	if m.console == nil || m.console.id != a.Info().ID || !m.console.focused || m.statusMsg == "" {
		t.Fatalf("no terminal: console=%+v msg=%q, want the agent kept and a status line", m.console, m.statusMsg)
	}
	term := startSecondSession(t, a, "Terminal", true)
	m = pressAlt(t, m, 't')
	if m.console == nil || m.console.id != term.Info().ID || !m.console.focused {
		t.Fatalf("console=%+v, want the terminal bound", m.console)
	}
	m = pressAlt(t, m, 't')
	if m.console == nil || m.console.id != term.Info().ID {
		t.Fatalf("console=%+v, want the only terminal kept", m.console)
	}
}

// alt+w then alt+a: the ring restarts from the worktree alt+w landed on —
// its own session, not the one the walk came from.
func TestAltAAfterAltWStartsFromTheViewedWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, wtB := addWorktree(t, m, "wtB")
	m, wtC := addWorktree(t, m, "wtC")
	installSessionManager(t)
	idB := startSessionIn(t, m, wtB, "B")
	idC := startSessionIn(t, m, wtC, "C")
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != idB || !m.console.focused {
		t.Fatalf("alt+a: console=%+v, want B (the first below home)", m.console)
	}
	m = pressAlt(t, m, 'w') // hides B's console (wtB stays on screen)
	m = pressAlt(t, m, 'w') // …and moves on to wtC
	if m.viewed != model.KeyOf(wtC) || m.console != nil || m.focus != panelBranches {
		t.Fatalf("alt+w ×2: viewed=%q console=%v focus=%v, want wtC, hidden, Branches", m.viewed, m.console != nil, m.focus)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != idC || !m.console.focused {
		t.Fatalf("alt+a after alt+w: console=%+v, want C (the viewed worktree's own)", m.console)
	}
	_ = domain.Sessions
}
