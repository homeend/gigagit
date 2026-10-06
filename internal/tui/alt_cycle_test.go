package tui

import (
	"testing"
	"time"
)

func pressAlt(t *testing.T, m Model, r rune) Model {
	t.Helper()
	mm, _ := m.Update(altKey(r))
	return mm.(Model)
}

// The ring ends on the screen the cycle started from: the stash list in the
// right column comes back, focus with it.
func TestAltAReturnsToTheStartingScreen(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	sv := &stashView{tag: "stash"}
	m.stashView, m.focus = sv, panelCommits
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != s.Info().ID || m.console.focused || m.stashView != nil {
		t.Fatalf("first alt+a: console=%+v stash=%v", m.console, m.stashView)
	}
	m = pressAlt(t, m, 'a')
	if m.console != nil || m.stashView != sv || m.focus != panelCommits {
		t.Fatalf("second alt+a must return: console=%+v stash=%v focus=%v", m.console, m.stashView, m.focus)
	}
}

// From a focused agent the walk starts at the agent used before it, never
// Touches, and its return stop is the screen before the agent was shown.
func TestAltAFromFocusedAgentReturnsToTheScreenBeforeIt(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	b := startSecondSession(t, a, "b", false)
	m.focus = panelWorktrees
	m, _ = m.openConsole(a.Info().ID) // a is now the most recent
	usedA, usedB := a.Info().LastUsed, b.Info().LastUsed
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != b.Info().ID || m.console.focused {
		t.Fatalf("alt+a in focused a: console=%+v, want b unfocused", m.console)
	}
	if !a.Info().LastUsed.Equal(usedA) || !b.Info().LastUsed.Equal(usedB) {
		t.Fatal("the walk touched a session")
	}
	m = pressAlt(t, m, 'a')
	if m.console != nil || m.focus != panelWorktrees {
		t.Fatalf("return stop: console=%+v focus=%v, want the Worktrees panel", m.console, m.focus)
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != a.Info().ID {
		t.Fatalf("after the return stop: console=%+v, want a", m.console)
	}
}

// One agent, focused: alt+a goes straight back.
func TestAltAWithOneFocusedAgentReturns(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	m.focus = panelBranches
	m, _ = m.openConsole(a.Info().ID)
	m = pressAlt(t, m, 'a')
	if m.console != nil || m.focus != panelBranches {
		t.Fatalf("console=%+v focus=%v", m.console, m.focus)
	}
}

// alt+t in the middle of an alt+a cycle walks terminals; the return point
// survives the switch of kind.
func TestAltTMidCycleKeepsTheReturnPoint(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`)
	term := startSecondSession(t, a, "Terminal", true)
	sv := &stashView{tag: "stash"}
	m.stashView, m.focus = sv, panelCommits
	m = pressAlt(t, m, 'a')
	m = pressAlt(t, m, 't')
	if m.console == nil || m.console.id != term.Info().ID {
		t.Fatalf("alt+t: console=%+v, want the terminal", m.console)
	}
	m = pressAlt(t, m, 't')
	if m.console != nil || m.stashView != sv {
		t.Fatalf("alt+t return stop: console=%+v stash=%v", m.console, m.stashView)
	}
}

// A ctrl+t-pinned panel is a full-screen return point: the console shows
// maximised, the pin comes back on return.
func TestAltAFromPinnedPanelShowsFullAndRestoresThePin(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	m.focus = panelBranches
	m = press(t, m, "ctrl+t")
	if !m.fullMaxActive() {
		t.Fatal("baseline: Branches should be pinned")
	}
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.maximized || m.console.focused || m.fullMaxed {
		t.Fatalf("console=%+v fullMaxed=%v", m.console, m.fullMaxed)
	}
	m = pressAlt(t, m, 'a')
	if m.console != nil || !m.fullMaxActive() || m.fullMax != panelBranches || m.focus != panelBranches {
		t.Fatalf("return: console=%+v pin=%v/%v focus=%v", m.console, m.fullMaxed, m.fullMax, m.focus)
	}
}

// gg takes alt+a even inside a focused console.
func TestFocusedConsoleGivesAltAToTheCycle(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	other := startSecondSession(t, s, "other", false)
	time.Sleep(2 * time.Millisecond)
	other.Touch()
	m, _ = m.openConsole(s.Info().ID) // s most recent, other next
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.console.id != other.Info().ID || m.console.focused {
		t.Fatalf("console = %+v: alt+a in a focused console cycles", m.console)
	}
}
