package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// paletteType opens the palette and types s into it, one rune per keypress.
func paletteType(t *testing.T, s string) (Model, *commandPalette) {
	t.Helper()
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, keyType(tea.KeyCtrlP))
	for _, r := range s {
		if r == ' ' {
			m, _ = send(m, keyType(tea.KeySpace))
			continue
		}
		m, _ = send(m, keyRunes(string(r)))
	}
	p := layerOf[*commandPalette](m)
	if p == nil {
		t.Fatalf("typing %q must leave the palette open", s)
	}
	return m, p
}

// Letters never move or close the palette — they type into the filter (j, k
// and q included), exactly like the . action menu.
func TestCommandPaletteLettersTypeIntoFilter(t *testing.T) {
	t.Parallel()
	_, p := paletteType(t, "jkq")
	if p.query != "jkq" {
		t.Fatalf("query = %q, want %q", p.query, "jkq")
	}
	if p.sel != 0 {
		t.Fatalf("j/k must not move the selection, sel = %d", p.sel)
	}
}

// Typing narrows the visible rows, case-insensitively, on the label.
func TestCommandPaletteTypingFiltersRows(t *testing.T) {
	t.Parallel()
	_, p := paletteType(t, "file h")
	vis := p.visible()
	if len(vis) != 1 || vis[0].label != "File history" {
		t.Fatalf("visible = %v, want only File history", paletteLabels(vis))
	}
}

// enter runs the selected row of the FILTERED list, not the full registry's.
func TestCommandPaletteEnterRunsFilteredRow(t *testing.T) {
	t.Parallel()
	m, _ := paletteType(t, "show com")
	m, _ = send(m, keyType(tea.KeyEnter))
	if layerOf[*gotoCommitPopup](m) == nil {
		t.Fatal("enter on the filtered Show commit row should open the goto-commit popup")
	}
}

// Arrows still move while a filter is active, clamped to the visible rows.
func TestCommandPaletteArrowsMoveWithinFilter(t *testing.T) {
	t.Parallel()
	m, p := paletteType(t, "file")
	n := len(p.visible())
	if n < 2 {
		t.Fatalf("fixture: want 2+ rows matching %q, got %d", "file", n)
	}
	for i := 0; i < n+3; i++ {
		m, _ = send(m, keyType(tea.KeyDown))
	}
	if p.sel != n-1 {
		t.Fatalf("sel = %d, want clamped to %d", p.sel, n-1)
	}
	m, _ = send(m, keyType(tea.KeyUp))
	if p.sel != n-2 {
		t.Fatalf("sel = %d after up, want %d", p.sel, n-2)
	}
}

// First esc clears an active filter; only the next esc closes the palette.
func TestCommandPaletteEscClearsFilterFirst(t *testing.T) {
	t.Parallel()
	m, p := paletteType(t, "find")
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*commandPalette](m) == nil {
		t.Fatal("first esc must clear the filter, not close the palette")
	}
	if p.query != "" {
		t.Fatalf("query = %q after esc, want empty", p.query)
	}
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("second esc should close the palette")
	}
}

// Backspace trims one rune and resets the selection.
func TestCommandPaletteBackspaceTrimsFilter(t *testing.T) {
	t.Parallel()
	m, p := paletteType(t, "fin")
	_, _ = send(m, keyType(tea.KeyBackspace))
	if p.query != "fi" {
		t.Fatalf("query = %q, want %q", p.query, "fi")
	}
}

// enter with no matching row is a no-op — the palette stays open.
func TestCommandPaletteEnterOnNoMatchIsNoop(t *testing.T) {
	t.Parallel()
	m, _ := paletteType(t, "zzzz")
	m, _ = send(m, keyType(tea.KeyEnter))
	if p := layerOf[*commandPalette](m); p == nil || p != m.topLayer() {
		t.Fatal("enter on an empty filter result must leave the palette on top")
	}
}

// The render shows the query, only the matching rows, and the filter hint.
func TestCommandPaletteRendersFilter(t *testing.T) {
	t.Parallel()
	m, _ := paletteType(t, "find")
	out := m.View()
	for _, want := range []string{"find█", "Find", "type to filter"} {
		if !strings.Contains(out, want) {
			t.Errorf("filtered palette render missing %q", want)
		}
	}
	if strings.Contains(out, "Open repo") {
		t.Error("filtered palette must not draw non-matching rows")
	}
	m, _ = paletteType(t, "zzzz")
	if !strings.Contains(m.View(), "(no match)") {
		t.Error("an empty filter result should render (no match)")
	}
}

func paletteLabels(cs []paletteCommand) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.label
	}
	return out
}
