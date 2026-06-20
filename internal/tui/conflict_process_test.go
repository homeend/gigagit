package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// conflictModel() (in conflict_popup_test.go) builds a Model whose status holds
// two unmerged files (uu.txt, md.txt).

func TestConflictProcessStartsAndLeaves(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)

	cp, ok := m.proc.(*conflictProcess)
	if !ok {
		t.Fatalf("start must fill the slot with a conflict process, got %T", m.proc)
	}
	if cp.st != confListing {
		t.Fatalf("must start in Listing, got state %d", cp.st)
	}
	if len(cp.files) != 2 {
		t.Fatalf("must carry the 2 conflicted files, got %d", len(cp.files))
	}

	// Leave releases the slot (start-by-detection re-offers it later).
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = u.(Model)
	if m.proc != nil {
		t.Fatal("Leave must release the slot")
	}
}

func TestConflictProcessEscLeaves(t *testing.T) {
	m := conflictModel()
	m, _ = startConflictProcess(m)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if m.proc != nil {
		t.Fatal("esc must release the slot")
	}
}

func TestConflictProcessNoStartWithoutConflicts(t *testing.T) {
	m := Model{sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	m, _ = startConflictProcess(m)
	if m.proc != nil {
		t.Fatal("no conflicts → no process")
	}
}
