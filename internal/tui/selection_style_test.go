package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/theme"
)

// NOTE: the tests in this file do NOT call t.Parallel() —
// lipgloss.SetColorProfile is process-global, so a parallel sibling's deferred
// reset would land mid-render and drop the ANSI codes they assert on (the same
// rule window_syntax_test.go and search_paint_test.go follow).

// With the role UNSET (the Terminal theme) the stripe flips reverse video
// against the row it lands on: inverted over an ordinary row, a HOLE over a
// reverse-video one (blame's cursor row). This mirrors currentHitStyle's
// contract, minus the bold — a range marks EXTENT, not one hit.
func TestSelectionStyleFlipsReverseWhenTheRoleIsUnset(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	s := buildStyles(theme.Terminal)
	plain := s.selectionStyle(lipgloss.NewStyle())
	if !plain.GetReverse() {
		t.Fatal("over an ordinary row the stripe must turn reverse ON")
	}
	if plain.GetBold() {
		t.Fatal("the stripe must not bold a whole range")
	}
	if got := plain.Render("x"); !hasSGR(got, "7") {
		t.Fatalf("no reverse SGR in %q", got)
	}

	hole := s.selectionStyle(s.selectedRow)
	if hole.GetReverse() {
		t.Fatal("over a reverse-video row the stripe must turn reverse OFF (a hole)")
	}
	if got := hole.Render("x"); hasSGR(got, "7") {
		t.Fatalf("the hole must carry no reverse SGR: %q", got)
	}
}

// With the role SET the stripe is an explicit background patch that clears
// reverse, so it reads the same over a plain row, a reversed row and the diff's
// cursor-row band.
func TestSelectionStyleUsesTheThemeBackgroundWhenSet(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	s := buildStyles(theme.Theme{Name: "t", Selection: "#264F78"})
	for _, base := range []lipgloss.Style{lipgloss.NewStyle(), s.selectedRow, s.diffCursorRow} {
		sel := s.selectionStyle(base)
		if sel.GetReverse() {
			t.Fatalf("the background patch must clear reverse, base reverse=%v", base.GetReverse())
		}
		if got := sel.GetBackground(); got != lipgloss.Color("#264F78") {
			t.Fatalf("background = %v, want the theme's selection_bg", got)
		}
		got := sel.Render("x")
		if !strings.Contains(got, "48;2;38;79;120") {
			t.Fatalf("no true-colour background escape in %q", got)
		}
		if ansi.Strip(got) != "x" {
			t.Fatalf("the patch changed the text: %q", ansi.Strip(got))
		}
	}
}

// The built-ins carry the editor-selection blues; Terminal leaves the role
// unset so it inherits the terminal's own inversion.
func TestSelectionRoleValues(t *testing.T) {
	if theme.Dark.Selection != "#264F78" {
		t.Fatalf("Dark.Selection = %q, want #264F78", theme.Dark.Selection)
	}
	if theme.Light.Selection != "#ADD6FF" {
		t.Fatalf("Light.Selection = %q, want #ADD6FF", theme.Light.Selection)
	}
	if theme.Terminal.Selection != "" {
		t.Fatalf("Terminal.Selection = %q, want empty", theme.Terminal.Selection)
	}
}
