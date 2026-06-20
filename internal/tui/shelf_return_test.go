package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

func shelfSwitcherModel() Model {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	return m.pushOverlay(newShelfPopup([]model.ShelfEntry{{ID: "s1"}}))
}

func TestRestoreEscReturnsToShelfSwitcher(t *testing.T) {
	m := shelfSwitcherModel()
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}) // restore (openShelfRestore pushes)
	m = u.(Model)
	if _, ok := m.overlayTop().(*shelfRestorePopup); !ok {
		t.Fatal("p must push the restore popup")
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if m.shelfSwitcher() == nil || m.overlayTop() != m.shelfSwitcher() {
		t.Fatal("esc must return to the shelf switcher")
	}
}
