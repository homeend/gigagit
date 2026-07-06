package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestHandleMaxKeyTogglesOnT(t *testing.T) {
	var p popupMax
	if !p.handleMaxKey(runeKey("T")) {
		t.Fatal(`"T" must be consumed`)
	}
	if !p.maxed() {
		t.Fatal(`"T" must set maximized`)
	}
	if !p.handleMaxKey(runeKey("T")) || p.maxed() {
		t.Fatal(`second "T" must toggle back off`)
	}
}

func TestHandleMaxKeyIgnoresOtherKeys(t *testing.T) {
	var p popupMax
	if p.handleMaxKey(runeKey("x")) {
		t.Fatal(`"x" must NOT be consumed`)
	}
	if p.maxed() {
		t.Fatal(`"x" must not set maximized`)
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
