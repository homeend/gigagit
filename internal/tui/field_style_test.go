package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// asciiProfile forces lipgloss to emit no color codes, so width-padding (a
// layout concern) survives but styling does not — lets tests assert geometry.
func asciiProfile(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestViewFieldFillsWidthAndIndents(t *testing.T) {
	asciiProfile(t)
	f := newTextField("ab\ncd")
	out := viewField("desc: ", f, false, 20)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out)
	}
	// First line: label + value filled to the field width (20-6 = 14).
	wantLine0 := "desc: ab" + strings.Repeat(" ", 12)
	if lines[0] != wantLine0 {
		t.Fatalf("line0 = %q, want %q", lines[0], wantLine0)
	}
	// The continuation line's value must start in the SAME column as the
	// first line's value (the screenshot bug: it was 2 columns to the left).
	col0 := strings.Index(lines[0], "ab")
	col1 := strings.Index(lines[1], "cd")
	if col0 != col1 {
		t.Fatalf("value columns differ: first=%d cont=%d (%q / %q)", col0, col1, lines[0], lines[1])
	}
	if col1 != len("desc: ") {
		t.Fatalf("continuation indent = %d, want %d", col1, len("desc: "))
	}
}

func TestViewFieldStyledWhenFocused(t *testing.T) {
	forceColor(t)
	f := newTextField("hi")
	out := viewField("name: ", f, true, 30)
	if !strings.Contains(out, "\x1b[") {
		t.Fatal("expected ANSI styling on the editable field, got none")
	}
	if got := ansi.Strip(out); !strings.Contains(got, "name: hi") {
		t.Fatalf("stripped output = %q, want it to contain %q", got, "name: hi")
	}
}

func TestViewFieldEmptyFocusedShowsCursor(t *testing.T) {
	forceColor(t)
	empty := newTextField("")
	focused := viewField("name: ", empty, true, 30)
	unfocused := viewField("name: ", empty, false, 30)
	// The cursor must produce a visible distinction even on an empty field;
	// otherwise the user can't see where they're typing into the blank slot.
	if focused == unfocused {
		t.Fatal("focused empty field renders identically to unfocused — cursor invisible")
	}
	if !strings.Contains(focused, "\x1b[") {
		t.Fatal("focused empty field has no styling at all")
	}
}

func TestViewFieldWrapsLongLineAligned(t *testing.T) {
	asciiProfile(t)
	// A single logical line longer than the field width must WRAP into multiple
	// display lines, each starting in the same column as the first (the Junie
	// bug: the wrapped tail landed at column 0 under the label).
	f := newTextField("abcdefghij")      // 10 runes
	out := viewField("d: ", f, false, 9) // prefix width 3 → field width 6
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 wrapped lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "d: abcdef" {
		t.Fatalf("line0 = %q, want %q", lines[0], "d: abcdef")
	}
	col0 := strings.Index(lines[0], "abcdef")
	col1 := strings.Index(lines[1], "ghij")
	if col0 != col1 {
		t.Fatalf("wrap continuation misaligned: first=%d cont=%d (%q/%q)", col0, col1, lines[0], lines[1])
	}
	for i, l := range lines { // every display line is exactly contentWidth (9)
		if lipgloss.Width(l) != 9 {
			t.Fatalf("line %d width = %d, want 9: %q", i, lipgloss.Width(l), l)
		}
	}
}

func TestStyledLinesCursorFollowsWrap(t *testing.T) {
	forceColor(t)
	f := newTextField("abcdefgh") // 8 runes
	f.cursor = 7                  // the 'h' — in the SECOND width-4 chunk
	foc := f.styledLines(true, 4)
	unf := f.styledLines(false, 4)
	if len(foc) != 2 || len(unf) != 2 {
		t.Fatalf("want 2 chunks each: foc=%d unf=%d", len(foc), len(unf))
	}
	if foc[0] != unf[0] {
		t.Fatalf("cursor must not render on the first chunk")
	}
	if foc[1] == unf[1] {
		t.Fatal("cursor must render on the second (wrapped) chunk, but it is identical to unfocused")
	}
}

func TestCommitPopupDescriptionColumnsAlign(t *testing.T) {
	asciiProfile(t)
	p := &commitPopup{amend: true}
	p.title = newTextField("mod 1")
	p.desc = newTextField("mod 2\nmod 3")
	p.field = 1
	out := renderCommitFields(p, 52, 0) // 0 = no description height cap
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("want >=3 lines (title, desc, cont), got %d: %q", len(lines), out)
	}
	descCol := strings.Index(lines[1], "mod 2")
	contCol := strings.Index(lines[2], "mod 3")
	if descCol < 0 || contCol < 0 {
		t.Fatalf("could not find values: desc=%q cont=%q", lines[1], lines[2])
	}
	if descCol != contCol {
		t.Fatalf("description value columns misaligned: first=%d cont=%d\n%q\n%q",
			descCol, contCol, lines[1], lines[2])
	}
}
