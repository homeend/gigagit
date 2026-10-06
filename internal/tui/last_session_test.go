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

func TestSessionsByLastUsedNewestFirstRunningOfOneKind(t *testing.T) {
	t.Parallel()
	at := func(min int) time.Time { return time.Date(2026, 9, 30, 12, min, 0, 0, time.UTC) }
	list := []domain.SessionInfo{
		{ID: "s1", State: domain.SessionRunning, LastUsed: at(55)},
		{ID: "s2", State: domain.SessionExited, LastUsed: at(59)}, // newest, but closed
		{ID: "s3", State: domain.SessionRunning, LastUsed: at(58)},
		{ID: "s4", State: domain.SessionRunning, LastUsed: at(56)},
		{ID: "s5", State: domain.SessionRunning, LastUsed: at(57), Terminal: true},
	}
	ids := func(l []domain.SessionInfo) (out []domain.SessionID) {
		for _, i := range l {
			out = append(out, i.ID)
		}
		return out
	}
	if got := ids(sessionsByLastUsed(list, false)); len(got) != 3 || got[0] != "s3" || got[1] != "s4" || got[2] != "s1" {
		t.Fatalf("agents = %v, want [s3 s4 s1]", got)
	}
	if got := ids(sessionsByLastUsed(list, true)); len(got) != 1 || got[0] != "s5" {
		t.Fatalf("terminals = %v, want [s5]", got)
	}
}

// alt+a shows the last-used agent UNFOCUSED; each further alt+a steps one
// further back in last-used order (wrapping); enter focuses the shown one,
// which makes it the most recent.
func TestAltACyclesAgentsByLastUseAndEnterPromotes(t *testing.T) {
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
		if m.console == nil || m.console.id != want.Info().ID || m.console.focused {
			t.Fatalf("%s: console = %+v, want %s shown unfocused", what, m.console, want.Info().Label)
		}
	}
	press(altKey('a'))
	shows(a, "first alt+a")
	press(altKey('a'))
	shows(b, "second alt+a")
	press(altKey('a'))
	shows(c, "third alt+a")
	press(altKey('a'))
	if m.console != nil {
		t.Fatalf("fourth alt+a returns to the starting screen, console = %+v", m.console)
	}
	press(altKey('a'))
	shows(a, "fifth alt+a starts over")
	press(altKey('a'))
	press(altKey('a'))
	shows(c, "back on the third")
	press(keyMsg("enter"))
	if !m.console.focused {
		t.Fatal("enter focuses the shown session")
	}
	press(ctrlBracket())
	if got := sessionsByLastUsed(domain.Sessions().List(), false); got[0].ID != c.Info().ID {
		t.Fatalf("after enter the most recent agent is %s, want c", got[0].Label)
	}
	press(keyMsg("esc")) // close the console: the next alt+a starts from the top
	press(altKey('a'))
	shows(c, "alt+a after enter")
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
