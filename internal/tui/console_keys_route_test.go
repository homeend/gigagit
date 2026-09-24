package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlBracket() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyCtrlCloseBracket} }
func ctrlBackslash() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlBackslash} }

func TestConsoleStateMachine(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	step := func(k tea.KeyMsg) {
		t.Helper()
		mm, _ := m.Update(k)
		m = mm.(Model)
	}
	step(ctrlBracket()) // docked focused → docked unfocused
	if m.console == nil || m.console.focused || m.console.maximized || m.focus != panelCommits {
		t.Fatalf("after ctrl+]: %+v focus=%v", m.console, m.focus)
	}
	step(keyMsg("enter")) // → focused
	if !m.console.focused {
		t.Fatal("enter on the unfocused console must focus it")
	}
	step(ctrlBracket())
	step(keyMsg("ctrl+t")) // → maximised + focused
	if !m.console.maximized || !m.console.focused {
		t.Fatalf("ctrl+t: %+v", m.console)
	}
	step(ctrlBracket()) // maximised focused → docked unfocused
	if m.console.maximized || m.console.focused {
		t.Fatalf("ctrl+] from maximised: %+v", m.console)
	}
	step(keyMsg("esc")) // → closed
	if m.console != nil {
		t.Fatal("esc on the unfocused console must close it")
	}
	if s.Info().State != 0 { // domain.SessionRunning
		t.Fatal("the session must keep running")
	}
}

func TestFocusedConsoleForwardsGGShortcuts(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	// The child echoes what it reads in raw mode so we can see every byte.
	// READY after stty: a key sent while the tty is still cooked would turn
	// ctrl+c into SIGINT and kill the shell.
	s := startTestSession(t, m, `stty raw -echo; printf READY; while :; do dd bs=1 count=1 2>/dev/null | od -An -c; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	for _, k := range []tea.KeyMsg{keyMsg("q"), {Type: tea.KeyCtrlC}, {Type: tea.KeyCtrlO}, {Type: tea.KeyCtrlP}, keyMsg("."), keyMsg("?"), keyMsg("esc"), keyMsg("ctrl+t")} {
		mm, cmd := m.Update(k)
		m = mm.(Model)
		if m.console == nil || !m.console.focused || m.console.maximized {
			t.Fatalf("%q changed console state: %+v", k.String(), m.console)
		}
		if m.topLayer() != nil || m.actionMenu != nil {
			t.Fatalf("%q opened a gg surface", k.String())
		}
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("%q quit gg", k.String())
			}
		}
	}
	waitScreen(t, s, "003") // ctrl+c arrived as ETX
	waitScreen(t, s, "017") // ctrl+o arrived as SI
}

func TestCtrlBackslashFromPanels(t *testing.T) {
	t.Skip("Task 7: sessionsPopup")
	m := loadedModel(t)
	mm, _ := m.Update(ctrlBackslash())
	m = mm.(Model)
	_ = m // Task 7 asserts *sessionsPopup here
}

func TestUnfocusedConsoleSwallowsCommitsKeys(t *testing.T) {
	m := loadedModelLinearCommits(t, 5) // j must have somewhere to go
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	mm, _ := m.Update(ctrlBracket())
	m = mm.(Model)
	before := m.sel[panelCommits]
	if m.panelLen(panelCommits) < 2 {
		t.Fatalf("fixture has %d commit rows; the test needs 2+", m.panelLen(panelCommits))
	}
	mm, _ = m.Update(keyMsg("j"))
	m = mm.(Model)
	if m.sel[panelCommits] != before || m.console == nil {
		t.Fatal("j on the unfocused console must not move the hidden Commits cursor")
	}
}
