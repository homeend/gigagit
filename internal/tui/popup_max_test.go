package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestToggleMaximize(t *testing.T) {
	var p popupMax
	p.toggleMaximize()
	if !p.maxed() {
		t.Fatal("toggleMaximize must set maximized")
	}
	p.toggleMaximize()
	if p.maxed() {
		t.Fatal("second toggleMaximize must clear maximized")
	}
}

func TestCapturingTextDefaultFalse(t *testing.T) {
	var p popupMax
	if p.capturingText() {
		t.Fatal("popupMax default capturingText must be false")
	}
}

// contentPopup embeds popupMax and, on the layer stack, is maximized by the
// central T handler in Model.Update — not by its own update. This locks in that
// the central handler is wired.
func TestCentralTMaximizesTopPopup(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := newContentPopup("Title", contentLines(4))
	m = m.pushLayer(p)

	mm, _ := m.Update(runeKey("T"))
	if !layerOf[*contentPopup](mm.(Model)).maxed() {
		t.Fatal("central T handler must maximize the top popup")
	}
	// Second T restores.
	mm2, _ := mm.(Model).Update(runeKey("T"))
	if layerOf[*contentPopup](mm2.(Model)).maxed() {
		t.Fatal("second central T must restore the top popup")
	}
}

// While a popup is capturing text, the central T handler must leave T a literal
// character (capturingText true) instead of maximizing.
func TestCentralTLiteralWhileCapturing(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := newContentPopup("Title", contentLines(4))
	p.typing = true
	m = m.pushLayer(p)

	mm, _ := m.Update(runeKey("T"))
	got := layerOf[*contentPopup](mm.(Model))
	if got.maxed() {
		t.Fatal("T while capturing text must not maximize")
	}
	if got.query != "T" {
		t.Fatalf("T while capturing must be typed as a literal; query=%q", got.query)
	}
}

func TestPopupResolveWidth(t *testing.T) {
	if got := popupResolveWidth(200, false, 56); got != 56 {
		t.Fatalf("normal: got %d, want 56", got)
	}
	if got := popupResolveWidth(200, true, 56); got != popupFullInnerWidth(200) {
		t.Fatalf("maximized: got %d, want %d", got, popupFullInnerWidth(200))
	}
}

func TestPopupMaxRowCap(t *testing.T) {
	if got := popupMaxRowCap(50); got != 38 {
		t.Fatalf("tall: got %d, want 38", got)
	}
	if got := popupMaxRowCap(5); got != 3 {
		t.Fatalf("floor: got %d, want 3", got)
	}
}

func TestPopupResolveRowCap(t *testing.T) {
	if got := popupResolveRowCap(false, 50, 12); got != 12 {
		t.Fatalf("normal: got %d, want 12", got)
	}
	if got := popupResolveRowCap(true, 50, 12); got != 38 { // popupMaxRowCap(50)=38 > 12
		t.Fatalf("maximized tall: got %d, want 38", got)
	}
	if got := popupResolveRowCap(true, 20, 12); got != 12 { // popupMaxRowCap(20)=8 < 12 → floor
		t.Fatalf("maximized short must floor to normal: got %d, want 12", got)
	}
}
