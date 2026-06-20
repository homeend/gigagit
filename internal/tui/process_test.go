package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// stubProcess is a test-only process used to prove the slot gate in isolation,
// before the real conflict process exists.
type stubProcess struct {
	keys []string
	left bool
}

func (p *stubProcess) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.String() == "q" {
		p.left = true
		m.proc = nil // release the slot
		return m, nil
	}
	p.keys = append(p.keys, msg.String())
	return m, nil
}
func (p *stubProcess) render(m Model, below string) string { return "STUBPROC" }
func (p *stubProcess) finished(m Model, res engine.Result, err error) (Model, tea.Cmd) {
	return m, nil
}
func (p *stubProcess) refreshed(m Model) (Model, tea.Cmd) { return m, nil }
func (p *stubProcess) indicator(m Model) string           { return "stub running" }

func TestProcessOwnsInputAndRender(t *testing.T) {
	p := &stubProcess{}
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}, proc: p}

	// A key that would normally open the bookmark switcher must reach the
	// process, not open anything.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = u.(Model)
	if m.topLayer() != nil {
		t.Fatal("no overlay may open while a process owns the slot")
	}
	if len(p.keys) != 1 || p.keys[0] != "g" {
		t.Fatalf("the process must receive the key, got %v", p.keys)
	}

	// Render is the process's.
	if got := m.render(); got != "STUBPROC" {
		t.Fatalf("render must be the process's, got %q", got)
	}

	// The process can release the slot.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = u.(Model)
	if m.proc != nil {
		t.Fatal("the process released the slot; proc must be nil")
	}
}
