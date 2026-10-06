package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const fortyLines = `i=0; while [ $i -lt 40 ]; do echo "row-$i"; i=$((i+1)); done; printf 'TAIL'; while :; do sleep 5; done`

// keys sends each key through Update.
func keys(m Model, ks ...tea.KeyMsg) Model {
	for _, k := range ks {
		nm, _ := m.Update(k)
		m = nm.(Model)
	}
	return m
}

var altPgUp = tea.KeyMsg{Type: tea.KeyPgUp, Alt: true}

func TestScrollWheelEntersAndShowsHistory(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	for range 20 {
		m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	}
	if m.console.scroll == nil || m.console.scroll.top != 0 {
		t.Fatalf("scroll = %+v", m.console.scroll)
	}
	if out := m.View(); !strings.Contains(out, "row-0") || strings.Contains(out, "TAIL") {
		t.Fatal("top of history not shown")
	}
}

func TestScrollWheelEntersOnUnfocusedConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m.console.focused, m.focus = false, panelBranches
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	if m.console.scroll == nil || m.focus != panelBranches {
		t.Fatalf("scroll=%v focus=%v", m.console.scroll != nil, m.focus)
	}
}

func TestScrollPastBottomLeaves(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelUp, tea.MouseActionPress)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelDown, tea.MouseActionPress)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelDown, tea.MouseActionPress)
	if m.console.scroll != nil {
		t.Fatal("still scrolling at the bottom")
	}
}

const hexEcho = `stty raw -echo; printf 'READY\n'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`

func TestScrollEscNeverReachesAgent(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, hexEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	m = keys(m, altPgUp)
	if m.console.scroll == nil {
		t.Fatal("alt+pgup did not enter scroll mode")
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.console.scroll != nil || !m.console.focused {
		t.Fatalf("esc: scroll=%v focused=%v", m.console.scroll != nil, m.console.focused)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	waitScreen(t, s, "7a")
	if strings.Contains(strings.Join(s.Screen().Lines, ""), "1b") {
		t.Fatal("esc (or alt+pgup) reached the agent")
	}
}

func TestScrollTypedKeyLeavesAndForwards(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, hexEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	m = keys(m, altPgUp, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.console.scroll != nil {
		t.Fatal("typing did not leave scroll mode")
	}
	waitScreen(t, s, "79")
}

func TestScrollKeysMoveCursorAndView(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m = keys(m, altPgUp)
	last := m.console.scroll.hist.Len() - 1
	if m.console.scroll.cursor != last {
		t.Fatalf("cursor %d, want %d", m.console.scroll.cursor, last)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyHome})
	if m.console.scroll.cursor != 0 || m.console.scroll.top != 0 {
		t.Fatalf("home: cursor=%d top=%d", m.console.scroll.cursor, m.console.scroll.top)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.console.scroll.cursor != 1 {
		t.Fatalf("down: cursor=%d", m.console.scroll.cursor)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.console.scroll == nil || m.console.scroll.cursor != last {
		t.Fatal("G did not go to the bottom (and must not leave)")
	}
}

func TestScrollUnfocusedPgUpEnters(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m.console.focused = false // its column keeps the focus
	m = keys(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.console.scroll == nil {
		t.Fatal("pgup on the unfocused console did not enter scroll mode")
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.console == nil || m.console.scroll != nil {
		t.Fatal("esc must leave scroll mode, not close the console")
	}
}

func TestScrollResizeLeaves(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m = keys(m, altPgUp)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = nm.(Model)
	if m.console.scroll != nil {
		t.Fatal("a resize kept the old-width snapshot")
	}
}

func TestScrollTitleShowsPositionAndNewOutput(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, `i=0; while [ $i -lt 40 ]; do echo "row-$i"; i=$((i+1)); done; read _; echo LATER; sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "row-39")
	m = keys(m, altPgUp)
	if out := m.View(); !strings.Contains(out, "scroll ↑") || strings.Contains(out, "new output") {
		t.Fatal("title before new output")
	}
	s.SendText("\r")
	waitScreen(t, s, "LATER")
	if out := m.View(); !strings.Contains(out, "new output") {
		t.Fatal("new output not flagged")
	}
}
