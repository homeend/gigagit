package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func TestShelfRestoreCursorEdit(t *testing.T) {
	t.Parallel()
	p := &shelfRestorePopup{entryID: "e1", origin: "a/b.txt"}
	m := Model{}
	m, _ = p.update(m, keyMsg("dir/file"))
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("left"))
	m, _ = p.update(m, keyMsg("X")) // insert two from end -> "dir/fiXle"
	_ = m
	if got := p.dest.Value(); got != "dir/fiXle" {
		t.Fatalf("dest = %q, want dir/fiXle", got)
	}
}

// p on a shelf entry must open the restore popup with the destination
// prefilled with the entry's origin path, cursor at the end.
func TestShelfRestorePrefillsOriginPath(t *testing.T) {
	t.Parallel()
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	m = m.pushLayer(newShelfPopup([]model.ShelfEntry{{ID: "s1", Origin: model.FileAddress{Path: "dir/a.go"}}}))
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = u.(Model)
	p, ok := m.topLayer().(*shelfRestorePopup)
	if !ok {
		t.Fatal("p must push the restore popup")
	}
	if got := p.dest.Value(); got != "dir/a.go" {
		t.Fatalf("dest = %q, want the origin path dir/a.go", got)
	}
	// cursor sits at the end: a typed rune appends
	m, _ = p.update(m, keyMsg("X"))
	if got := p.dest.Value(); got != "dir/a.goX" {
		t.Fatalf("after typing, dest = %q, want dir/a.goX (cursor at end)", got)
	}
}

// ctrl+r re-fills the destination with the origin path after the user
// mangled or cleared it.
func TestShelfRestoreCtrlRRefillsOrigin(t *testing.T) {
	t.Parallel()
	p := &shelfRestorePopup{entryID: "e1", origin: "dir/a.go", dest: newTextField("dir/a.go")}
	m := Model{}
	for i := 0; i < len("dir/a.go"); i++ {
		m, _ = p.update(m, keyMsg("backspace"))
	}
	if got := p.dest.Value(); got != "" {
		t.Fatalf("setup: dest = %q, want empty", got)
	}
	m, _ = p.update(m, keyMsg("ctrl+r"))
	if got := p.dest.Value(); got != "dir/a.go" {
		t.Fatalf("after ctrl+r, dest = %q, want dir/a.go", got)
	}
	// re-fill leaves the cursor at the end so the user can keep typing
	m, _ = p.update(m, keyMsg("X"))
	_ = m
	if got := p.dest.Value(); got != "dir/a.goX" {
		t.Fatalf("after ctrl+r + typing, dest = %q, want dir/a.goX", got)
	}
}
