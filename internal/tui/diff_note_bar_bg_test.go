package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The bar cell wears the gutter's background: on a cursor row the number is
// drawn on the cursor colour, and a bar on the terminal's own background would
// punch a one-column hole in it. Not parallel: the colour profile is global.
func TestNoteBarCellKeepsTheGutterBackground(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	gut := lipgloss.NewStyle().Background(lipgloss.Color("#102030"))
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	mk := cellMark{gut: gut, note: true, noteBar: bar}
	got := mk.gutterCell("12 ", 2)
	want := gut.Render("12") + bar.Background(lipgloss.Color("#102030")).Render(noteBarGlyph)
	if got != want {
		t.Fatalf("bar cell\n got %q\nwant %q", got, want)
	}
	if strings.Count(got, "48;2;16;32;48") != 2 {
		t.Fatalf("the gutter background must cover the number AND the bar: %q", got)
	}
	// No background on the gutter: the bar adds none.
	plain := cellMark{gut: lipgloss.NewStyle(), note: true, noteBar: bar}.gutterCell("12 ", 2)
	if strings.Contains(plain, "48;") {
		t.Fatalf("a plain gutter got a background: %q", plain)
	}
}
