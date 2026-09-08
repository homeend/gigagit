package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

func TestSanitizeSpansMapsThroughTabExpansion(t *testing.T) {
	// "\tx" — a leading tab expands to 4 spaces; the span over 'x' (raw rune
	// index 1) must land on display column 4, not column 1.
	disp, emph, _ := sanitizeCell("\tx", []textdiff.Span{{Start: 1, End: 2}}, nil)
	if string(disp) != "    x" {
		t.Fatalf("disp = %q, want %q", string(disp), "    x")
	}
	want := []bool{false, false, false, false, true}
	for i := range want {
		if emph[i] != want[i] {
			t.Fatalf("emph = %v, want %v", emph, want)
		}
	}
}

func TestSanitizeSpansControlCharBecomesDot(t *testing.T) {
	disp, _, _ := sanitizeCell("a\x01b", nil, nil)
	if string(disp) != "a·b" {
		t.Fatalf("disp = %q, want %q", string(disp), "a·b")
	}
}

func TestCoverMaskClampsEnds(t *testing.T) {
	m := coverMask(3, []textdiff.Span{{Start: 1, End: 99}})
	want := []bool{false, true, true}
	for i := range want {
		if m[i] != want[i] {
			t.Fatalf("mask = %v, want %v", m, want)
		}
	}
}

func TestSanitizeCellCarriesClassesThroughTabs(t *testing.T) {
	t.Parallel()
	// "\tif x" — the keyword `if` sits at raw runes [1,3); the tab expands to 4 cols.
	disp, emph, cls := sanitizeCell("\tif x", nil, []syntax.Tok{{Start: 1, End: 3, Class: syntax.Keyword}})
	if string(disp) != "    if x" {
		t.Fatalf("disp = %q", string(disp))
	}
	if len(cls) != len(disp) || len(emph) != len(disp) {
		t.Fatalf("masks must parallel disp: cls=%d emph=%d disp=%d", len(cls), len(emph), len(disp))
	}
	want := []syntax.Class{0, 0, 0, 0, syntax.Keyword, syntax.Keyword, 0, 0}
	for i := range want {
		if cls[i] != want[i] {
			t.Errorf("cls[%d] = %v, want %v (%v)", i, cls[i], want[i], cls)
			break
		}
	}
}
