package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+h":
		return tea.KeyMsg{Type: tea.KeyCtrlH}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+up":
		return tea.KeyMsg{Type: tea.KeyCtrlUp}
	case "ctrl+down":
		return tea.KeyMsg{Type: tea.KeyCtrlDown}
	case "ctrl+left":
		return tea.KeyMsg{Type: tea.KeyCtrlLeft}
	case "ctrl+right":
		return tea.KeyMsg{Type: tea.KeyCtrlRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "alt+down":
		return tea.KeyMsg{Type: tea.KeyDown, Alt: true}
	case "alt+up":
		return tea.KeyMsg{Type: tea.KeyUp, Alt: true}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestQuitOnQ(t *testing.T) {
	m := New(nil)
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("expected a command from pressing q")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("pressing q should issue tea.Quit")
	}
}

func TestWindowSizeIsRecorded(t *testing.T) {
	m := New(nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := updated.(Model)
	if mm.width != 120 || mm.height != 40 {
		t.Fatalf("size = %dx%d, want 120x40", mm.width, mm.height)
	}
}
