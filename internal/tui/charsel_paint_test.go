package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/theme"
)

// NOTE: sets the process-global colour profile — not parallel (see
// diff_select_test.go).

// emphSel paints the selection stripe, emphSelCur the cursor cell over it;
// overlayHits maps a sel span to them (the cursor span wins).
func TestCharSelLevelsPaintStripeAndCursor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark) // selection_bg set: the stripe is a background patch
	s := st()
	base := lipgloss.NewStyle()
	got := styledRuns([]rune("abc"), []emphLevel{emphNone, emphSel, emphSelCur}, make([]syntax.Class, 3), base) // both masks full-length, as colouredLine hands them
	wantB := s.selectionStyle(base).Render("b")
	wantC := s.currentHitStyle(s.selectionStyle(base)).Render("c")
	if !strings.Contains(got, wantB) || !strings.Contains(got, wantC) || strings.HasPrefix(got, "\x1b") {
		t.Fatalf("got %q\nwant a plain a, then %q, then %q", got, wantB, wantC)
	}
	emph := overlayHits(nil, 0, 4, []hitSpan{{start: 1, end: 4, sel: true}, {start: 2, end: 3, sel: true, cur: true}})
	if !equalEmph(emph, []emphLevel{emphNone, emphSel, emphSelCur, emphSel}) {
		t.Fatalf("overlay = %v", emph)
	}
	if emph := overlayHits(nil, 0, 2, []hitSpan{{start: 0, end: 2, cur: true}}); !equalEmph(emph, []emphLevel{emphCur, emphCur}) {
		t.Fatalf("a search hit still maps to emphCur: %v", emph)
	}
}

// F1: in the terminal theme (selection_bg and search_current_bg unset) the
// cursor must show before a start is fixed: emphCur reverses the cell.
func TestCharSelCursorIsVisibleInTheTerminalTheme(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Terminal)
	s := st()
	base := lipgloss.NewStyle()
	got := styledRuns([]rune("ab"), []emphLevel{emphNone, emphCur}, make([]syntax.Class, 2), base)
	if want := s.currentHitStyle(base).Render("b"); !strings.Contains(got, want) || !strings.Contains(want, "7m") {
		t.Fatalf("got %q, want the cursor reversed (%q)", got, want)
	}
	// Inside a stripe (reverse on) the cursor flips back off: a hole, still visible.
	inside := styledRuns([]rune("ab"), []emphLevel{emphSel, emphSelCur}, make([]syntax.Class, 2), base)
	if !strings.Contains(inside, s.selectionStyle(base).Render("a")) || !strings.Contains(inside, s.currentHitStyle(s.selectionStyle(base)).Render("b")) {
		t.Fatalf("inside = %q", inside)
	}
}
