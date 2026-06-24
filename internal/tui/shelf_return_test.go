package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func shelfSwitcherModel() Model {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	return m.pushLayer(newShelfPopup([]model.ShelfEntry{{ID: "s1"}}))
}

// B1 (shelf): while an op runs the shelf switcher is inert — a keypress must not
// push the restore popup or launch a second op.
func TestShelfSwitcherInertWhileRunning(t *testing.T) {
	m := shelfSwitcherModel()
	m.running = true
	m.opMsgs = make(chan tea.Msg, 1)
	before := m.opMsgs
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = u.(Model)
	if cmd != nil || m.opMsgs != before {
		t.Fatal("a keypress while running must be a no-op and not replace opMsgs")
	}
	if _, ok := m.topLayer().(*shelfRestorePopup); ok {
		t.Fatal("p must not open the restore popup while running")
	}
}

func TestRestoreEscReturnsToShelfSwitcher(t *testing.T) {
	m := shelfSwitcherModel()
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}) // restore (openShelfRestore pushes)
	m = u.(Model)
	if _, ok := m.topLayer().(*shelfRestorePopup); !ok {
		t.Fatal("p must push the restore popup")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if m.shelfSwitcher() == nil || m.topLayer() != m.shelfSwitcher() {
		t.Fatal("esc must return to the shelf switcher")
	}
}
