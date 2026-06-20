package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// a trivial overlay for stack tests.
type fakeOverlay struct{ id string }

func (fakeOverlay) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) { return m, nil }
func (o fakeOverlay) render(m Model, below string) string           { return below + o.id }

func TestOverlayStackPushPopTop(t *testing.T) {
	var m Model
	if m.overlayTop() != nil {
		t.Fatal("empty stack must have nil top")
	}
	m = m.pushOverlay(fakeOverlay{id: "a"})
	m = m.pushOverlay(fakeOverlay{id: "b"})
	if got := m.overlayTop().(fakeOverlay).id; got != "b" {
		t.Fatalf("top = %q, want b", got)
	}
	m = m.popOverlay()
	if got := m.overlayTop().(fakeOverlay).id; got != "a" {
		t.Fatalf("after pop, top = %q, want a", got)
	}
	m = m.clearOverlays()
	if m.overlayTop() != nil {
		t.Fatal("clearOverlays must empty the stack")
	}
}

func TestPopOverlayEmptyIsNoOp(t *testing.T) {
	var m Model
	m = m.popOverlay() // must not panic
	if m.overlayTop() != nil {
		t.Fatal("pop on empty stack must stay empty")
	}
}

func TestOverlayOfReturnsTypedTopOrNil(t *testing.T) {
	var m Model
	// empty stack → nil
	if got := overlayOf[*bookmarkPastePopup](m); got != nil {
		t.Fatalf("empty stack: want nil, got %v", got)
	}
	want := &bookmarkPastePopup{origin: "a.go"}
	m = m.pushOverlay(want)
	if got := overlayOf[*bookmarkPastePopup](m); got != want {
		t.Fatalf("after push: want %p, got %p", want, got)
	}
	// a different concrete type is not matched
	if got := overlayOf[*bookmarkPopup](m); got != nil {
		t.Fatalf("wrong type: want nil, got %v", got)
	}
}
