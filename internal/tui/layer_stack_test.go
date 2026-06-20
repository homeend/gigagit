package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeLayer is a trivial layer for stack tests: its render composites its id onto
// `below` (so a walk's order is observable), and update records that it ran.
type fakeLayer struct {
	id      string
	updated bool
}

func (l *fakeLayer) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	l.updated = true
	return m, nil
}
func (l *fakeLayer) render(m Model, below string) string { return below + l.id }

func TestLayerStackPushPopTop(t *testing.T) {
	var m Model
	if m.topLayer() != nil {
		t.Fatal("empty stack must have nil top")
	}
	a, b := &fakeLayer{id: "a"}, &fakeLayer{id: "b"}
	m = m.pushLayer(a)
	if m.topLayer() != a {
		t.Fatal("push did not set top")
	}
	m = m.pushLayer(b)
	if m.topLayer() != b {
		t.Fatalf("top = %v, want b", m.topLayer())
	}
	m = m.popLayer()
	if m.topLayer() != a {
		t.Fatalf("after pop, top = %v, want a", m.topLayer())
	}
	m = m.clearLayers()
	if m.topLayer() != nil {
		t.Fatal("clearLayers must empty the stack")
	}
}

func TestPopLayerEmptyIsNoOp(t *testing.T) {
	var m Model
	m = m.popLayer() // must not panic
	if m.topLayer() != nil {
		t.Fatal("pop on empty stack must stay empty")
	}
}

func TestLayerOfReturnsTypedTopOrNil(t *testing.T) {
	var m Model
	// empty stack → nil
	if got := layerOf[*bookmarkPastePopup](m); got != nil {
		t.Fatalf("empty stack: want nil, got %v", got)
	}
	want := &bookmarkPastePopup{origin: "a.go"}
	m = m.pushLayer(want)
	if got := layerOf[*bookmarkPastePopup](m); got != want {
		t.Fatalf("after push: want %p, got %p", want, got)
	}
	// a different concrete type is not matched
	if got := layerOf[*bookmarkPopup](m); got != nil {
		t.Fatalf("wrong type: want nil, got %v", got)
	}
}
