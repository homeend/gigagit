package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// startSecondSession starts another session on the manager first lives on
// (startTestSession swaps in a fresh manager per call).
func startSecondSession(t *testing.T, first *domain.AgentSession, label string, terminal bool) *domain.AgentSession {
	t.Helper()
	spec := sessionSpecForTest(label, first.Info().Dir)
	spec.Terminal = terminal
	s, err := domain.Sessions().Start(spec)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// alt+a walks the agents of a worktree in start order, whatever was used
// last, binding each; after the last the first again; esc leaves, and the
// next alt+a starts over at the viewed worktree's first.
func TestAltAWalksAgentsInStartOrderWhateverWasUsedLast(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	a := startTestSession(t, m, `sleep 5`) // swaps in the test manager
	b := startSecondSession(t, a, "b", false)
	c := startSecondSession(t, a, "c", false)
	term := startSecondSession(t, a, "Terminal", true)
	for _, s := range []*domain.AgentSession{c, b, a, term} { // last used: term, a, b, c
		time.Sleep(2 * time.Millisecond)
		s.Touch()
	}
	press := func(k tea.KeyMsg) {
		t.Helper()
		mm, _ := m.Update(k)
		m = mm.(Model)
	}
	shows := func(want *domain.AgentSession, what string) {
		t.Helper()
		if m.console == nil || m.console.id != want.Info().ID || !m.console.focused {
			t.Fatalf("%s: console = %+v, want %s bound", what, m.console, want.Info().Label)
		}
	}
	press(altKey('a'))
	shows(a, "first alt+a")
	press(altKey('a'))
	shows(b, "second alt+a")
	press(altKey('a'))
	shows(c, "third alt+a")
	press(altKey('a'))
	shows(a, "fourth alt+a goes round")
	press(ctrlBracket())
	press(keyMsg("esc")) // leave the console: the next alt+a starts over
	if m.console != nil {
		t.Fatalf("esc: console = %+v", m.console)
	}
	press(altKey('a'))
	shows(a, "alt+a after esc")
	press(altKey('t'))
	shows(term, "alt+t")
}

func TestAltASkipsAnExitedSession(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	older := startTestSession(t, m, `sleep 5`)
	newer := startSecondSession(t, older, "newer", false)
	time.Sleep(2 * time.Millisecond)
	newer.Touch()
	if err := domain.Sessions().Kill(newer.Info().ID); err != nil {
		t.Fatal(err)
	}
	<-newer.Done()
	mm, _ := m.Update(altKey('a'))
	m = mm.(Model)
	if m.console == nil || m.console.id != older.Info().ID {
		t.Fatalf("console = %+v, want the older session once the newer exited", m.console)
	}
}

func TestAltAWithNoRunningSessionSaysSo(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `exit 0`)
	<-s.Done()
	for _, r := range []rune{'a', 't'} {
		m.statusMsg = ""
		mm, _ := m.Update(altKey(r))
		m = mm.(Model)
		if m.console != nil || m.statusMsg == "" {
			t.Fatalf("alt+%c: console=%+v status=%q", r, m.console, m.statusMsg)
		}
	}
}

// Only focusing a console is "using" it: showing one while cycling must not
// reorder the list under the user's feet.
func TestShowingUnfocusedDoesNotTouch(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	before := s.Info().LastUsed
	time.Sleep(2 * time.Millisecond)
	m, _ = m.showConsole(s.Info().ID, false)
	if got := s.Info().LastUsed; !got.Equal(before) {
		t.Fatalf("LastUsed moved %v → %v on an unfocused show", before, got)
	}
	m, _ = m.openConsole(s.Info().ID)
	if got := s.Info().LastUsed; !got.After(before) {
		t.Fatal("a focused open is a use")
	}
}
