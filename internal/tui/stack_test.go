package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeSurface records whether it owned the last update.
type fakeSurface struct{ updated bool }

func (s *fakeSurface) render(m Model, _ string) string { return "FAKE" }
func (s *fakeSurface) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	s.updated = true
	return m, nil
}

func TestStackPushPopOwnership(t *testing.T) {
	m := Model{}
	if m.stackTop() != nil {
		t.Fatal("empty stack should have no top")
	}
	s := &fakeSurface{}
	m = m.pushSurface(s)
	if m.stackTop() != s {
		t.Fatal("push did not set top")
	}
	m = m.popSurface()
	if m.stackTop() != nil {
		t.Fatal("pop did not clear top")
	}
}
