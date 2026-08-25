package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestSafeRowTextNeutralizesTerminalWideSymbols guards the fix for the commit-row
// overflow bug: ☰ (U+2630) and its symbol-block kin measure as one cell to
// lipgloss/uniseg but terminals draw them as two, so an unsanitized subject
// overflows the panel border and wraps, desyncing the renderer.
func TestSafeRowTextNeutralizesTerminalWideSymbols(t *testing.T) {
	t.Parallel()
	// The trigger from this repo's own wave-3 merge subjects.
	got := safeRowText("global ☰ menu")
	if want := "global ? menu"; got != want {
		t.Fatalf("safeRowText(☰) = %q, want %q", got, want)
	}
	// After sanitizing, lipgloss.Width equals the rune count — the property the
	// panel truncation relies on to keep a row inside its border.
	if w := lipgloss.Width(got); w != len([]rune(got)) {
		t.Fatalf("sanitized width = %d, want %d (all width-1)", w, len([]rune(got)))
	}
}

func TestWidthUnsafeClassification(t *testing.T) {
	t.Parallel()
	// Text-presentation symbols in the guarded blocks that lipgloss/uniseg still
	// measure as one cell (so terminals silently draw them wider) → unsafe. Only
	// runes that actually measure width-1 qualify; a symbol uniseg already widths
	// as 2 (e.g. ⭐ U+2B50) is excluded by the guard's own lipgloss.Width check.
	for _, r := range []rune{0x2630 /*☰*/, 0x2600 /*☀*/, 0x2666 /*♦*/} {
		if lipgloss.Width(string(r)) != 1 {
			t.Fatalf("test fixture %#x is not width-1; pick another", r)
		}
		if !widthUnsafe(r) {
			t.Errorf("widthUnsafe(%#x) = false, want true", r)
		}
	}
	// gg's own row markers and plain text must NOT be rewritten: ● ◇ live in the
	// Geometric Shapes block, ↓ in Arrows, ² in Latin-1, and ASCII is width-1.
	for _, r := range []rune{'●', '◇', '↓', '²', '—', 'a', ' ', '/'} {
		if widthUnsafe(r) {
			t.Errorf("widthUnsafe(%q %#x) = true, want false", r, r)
		}
	}
	// A properly-encoded emoji already measures as 2, so it is left alone.
	if widthUnsafe('🚀') {
		t.Error("widthUnsafe(🚀) = true; proper emoji already measure wide, leave them")
	}
}
