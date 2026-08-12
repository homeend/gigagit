package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// plain strips ANSI so assertions compare visible text.
func plain(s string) string { return ansi.Strip(s) }

func TestTwoColCutoffTruncates(t *testing.T) {
	rows := []colRow{{
		left:  &winCell{gutter: "[ ] ", body: "abcdefghijklmnop"},
		right: &winCell{gutter: "[ ] ", body: "right"},
	}}
	out, _ := renderTwoCol(rows, twoColOpts{w: 23, h: 1, sep: " | ", mode: modeCutoff})
	if len(out) != 1 {
		t.Fatalf("want 1 line, got %d", len(out))
	}
	// colW = (23-3)/2 = 10; left cell "[ ] abcde…" fits 10 cols with ellipsis.
	if !strings.Contains(plain(out[0]), "…") {
		t.Fatalf("cutoff should ellipsize long body: %q", plain(out[0]))
	}
	if !strings.Contains(plain(out[0]), "[ ] ") {
		t.Fatalf("gutter must be present: %q", plain(out[0]))
	}
}

func TestTwoColScrollReveals(t *testing.T) {
	rows := []colRow{{
		left:  &winCell{gutter: "", body: "0123456789ABCDEF"},
		right: &winCell{gutter: "", body: ""},
	}}
	o := twoColOpts{w: 23, h: 1, sep: " | ", mode: modeScroll, hscroll: 5}
	out, _ := renderTwoCol(rows, o)
	// colW=10; scrolled by 5 → left shows "56789ABCDE" (no leading 0).
	if strings.Contains(plain(out[0]), "0123") {
		t.Fatalf("scroll should hide the start: %q", plain(out[0]))
	}
	if !strings.Contains(plain(out[0]), "56789") {
		t.Fatalf("scroll should reveal the offset slice: %q", plain(out[0]))
	}
}

func TestTwoColWrapAlignsPairsAndGutterOnlyFirst(t *testing.T) {
	rows := []colRow{{
		left:  &winCell{gutter: "[x] ", body: "aaa bbb ccc"},
		right: &winCell{gutter: "[ ] ", body: "z"},
	}}
	// colW=10 → bodyW=6 → left wraps "aaa bb"/"b ccc" (2 segs); right is 1 seg.
	out, _ := renderTwoCol(rows, twoColOpts{w: 23, h: 4, sep: " | ", mode: modeWrap})
	if len(out) != 4 {
		t.Fatalf("want h=4 lines, got %d", len(out))
	}
	// First display line has both gutters.
	if !strings.Contains(plain(out[0]), "[x] ") || !strings.Contains(plain(out[0]), "[ ] ") {
		t.Fatalf("first wrap line needs both gutters: %q", plain(out[0]))
	}
	// Second display line: left continues (no gutter), right is blank-padded —
	// the pair stays registered (left's 2nd seg sits beside a blank right cell).
	if strings.Contains(plain(out[1]), "[x]") {
		t.Fatalf("continuation line must not repeat the gutter: %q", plain(out[1]))
	}
}

func TestTwoColVerticalWindowKeepsAnchor(t *testing.T) {
	var rows []colRow
	for i := 0; i < 20; i++ {
		rows = append(rows, colRow{full: &winCell{body: string(rune('A' + i))}})
	}
	out, _ := renderTwoCol(rows, twoColOpts{w: 20, h: 5, sep: " | ", mode: modeCutoff, anchor: 18})
	if len(out) != 5 {
		t.Fatalf("want 5 lines, got %d", len(out))
	}
	joined := plain(strings.Join(out, "\n"))
	if !strings.Contains(joined, "S") { // 'A'+18 == 'S'
		t.Fatalf("anchor row 18 ('S') must be visible: %q", joined)
	}
}

func TestTwoColVShiftSlidesAndClamps(t *testing.T) {
	var rows []colRow
	for i := 0; i < 20; i++ {
		rows = append(rows, colRow{full: &winCell{body: string(rune('A' + i))}})
	}
	o := twoColOpts{w: 20, h: 5, sep: " | ", mode: modeCutoff, anchor: 0}

	o.vshift = 3
	out, eff := renderTwoCol(rows, o)
	joined := plain(strings.Join(out, "\n"))
	if eff != 3 {
		t.Fatalf("eff = %d, want 3", eff)
	}
	if strings.Contains(joined, "A") || !strings.Contains(joined, "D") || !strings.Contains(joined, "H") {
		t.Fatalf("vshift 3 should show D..H: %q", joined)
	}

	o.vshift = 999
	_, eff = renderTwoCol(rows, o)
	if eff != 15 { // clamped to the last page: start 15 of len 20, h 5
		t.Fatalf("eff = %d, want 15", eff)
	}

	o.anchor, o.vshift = 18, -999
	out, eff = renderTwoCol(rows, o)
	if eff != -15 { // anchored start 15 (windowStart(20,5,18)), clamped to 0
		t.Fatalf("eff = %d, want -15", eff)
	}
	if !strings.Contains(plain(out[0]), "A") {
		t.Fatalf("clamped to top must show A: %q", plain(out[0]))
	}

	o.anchor, o.vshift = 0, 7
	_, eff = renderTwoCol(rows[:3], o)
	if eff != 0 { // content shorter than h: nothing to shift
		t.Fatalf("short content: eff = %d, want 0", eff)
	}
}
