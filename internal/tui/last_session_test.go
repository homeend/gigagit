package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

func ctrlA() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlA} }

// startSecondSession starts another session on the manager first lives on
// (startTestSession swaps in a fresh manager per call).
func startSecondSession(t *testing.T, first *domain.AgentSession) *domain.AgentSession {
	t.Helper()
	s, err := domain.Sessions().Start(sessionSpecForTest("second", first.Info().Dir))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLastUsedRunningSessionPicksTheNewestUse(t *testing.T) {
	t.Parallel()
	at := func(min int) time.Time { return time.Date(2026, 9, 30, 12, min, 0, 0, time.UTC) }
	list := []domain.SessionInfo{
		{ID: "s1", State: domain.SessionRunning, LastUsed: at(55)}, // 5 minutes ago
		{ID: "s2", State: domain.SessionExited, LastUsed: at(59)},  // newest, but closed
		{ID: "s3", State: domain.SessionRunning, LastUsed: at(58)}, // 2 minutes ago
		{ID: "s4", State: domain.SessionRunning, LastUsed: at(56)},
	}
	if got, ok := lastUsedRunningSession(list); !ok || got.ID != "s3" {
		t.Fatalf("got %v %v, want s3", got.ID, ok)
	}
	if _, ok := lastUsedRunningSession(list[1:2]); ok {
		t.Fatal("an exited session is never picked")
	}
}

func TestCtrlAOpensTheLastUsedRunningSession(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	older := startTestSession(t, m, `sleep 5`) // swaps in the test manager
	newer := startSecondSession(t, older)
	press := func() {
		t.Helper()
		mm, _ := m.Update(ctrlA())
		m = mm.(Model)
	}
	time.Sleep(2 * time.Millisecond)
	newer.Touch()
	press()
	if m.console == nil || m.console.id != newer.Info().ID || !m.console.focused {
		t.Fatalf("console = %+v, want the newer session focused", m.console)
	}
	// A focused console owns ctrl+a (readline's start-of-line): step out first.
	mm, _ := m.Update(ctrlBracket())
	m = mm.(Model)
	time.Sleep(2 * time.Millisecond)
	older.Touch()
	press() // the docked, unfocused console's column lets ctrl+a through
	if m.console == nil || m.console.id != older.Info().ID || !m.console.focused {
		t.Fatalf("console = %+v, want the older session after it was used last", m.console)
	}
	// The last-used one closes: ctrl+a falls back to the next most recent.
	mm, _ = m.Update(ctrlBracket())
	m = mm.(Model)
	if err := domain.Sessions().Kill(older.Info().ID); err != nil {
		t.Fatal(err)
	}
	<-older.Done()
	press()
	if m.console == nil || m.console.id != newer.Info().ID {
		t.Fatalf("console = %+v, want the newer session once the older exited", m.console)
	}
}

func TestCtrlAWithNoRunningSessionSaysSo(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `exit 0`)
	<-s.Done()
	mm, _ := m.Update(ctrlA())
	m = mm.(Model)
	if m.console != nil || m.statusMsg == "" {
		t.Fatalf("console=%+v status=%q", m.console, m.statusMsg)
	}
}

func TestFocusedConsoleKeepsCtrlA(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	other := startSecondSession(t, s)
	m, _ = m.openConsole(s.Info().ID)
	time.Sleep(2 * time.Millisecond)
	other.Touch()
	mm, _ := m.Update(ctrlA())
	m = mm.(Model)
	if m.console == nil || m.console.id != s.Info().ID {
		t.Fatalf("console = %+v: ctrl+a in a focused console belongs to the agent", m.console)
	}
}
