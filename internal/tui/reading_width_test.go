package tui

import (
	"strings"
	"testing"
)

func TestReadingColumn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ textW, rw, col, margin int }{
		{200, 120, 120, 40},
		{121, 120, 120, 0}, // one spare column: no half-cell centring
		{100, 120, 100, 0}, // narrower than the width: untouched
		{200, 10, 40, 80},  // floored at readingWidthMin
	} {
		col, margin := readingColumn(tc.textW, tc.rw)
		if col != tc.col || margin != tc.margin {
			t.Errorf("readingColumn(%d, %d) = %d, %d; want %d, %d", tc.textW, tc.rw, col, margin, tc.col, tc.margin)
		}
	}
}

// textSpan is the [first, last] column any non-space text occupies across
// the box's inner lines (border rows skipped, the │ frame cut off).
func textSpan(t *testing.T, box string) (lo, hi int) {
	t.Helper()
	lo, hi = 1<<30, -1
	for _, l := range strings.Split(plain(box), "\n") {
		r := []rune(l)
		if len(r) < 3 || (r[0] != '│' && r[0] != '║') {
			continue
		}
		inner := string(r[1 : len(r)-1])
		s := strings.TrimRight(inner, " ")
		if s == "" {
			continue
		}
		if a := len([]rune(s)) - len([]rune(strings.TrimLeft(s, " "))); a < lo {
			lo = a
		}
		if b := len([]rune(s)); b > hi {
			hi = b
		}
	}
	if hi < 0 {
		t.Fatalf("no framed text lines in:\n%s", plain(box))
	}
	return lo, hi
}

// A maximized prose popup keeps its fullscreen frame but lays its text in a
// reading column, left-aligned inside it and centred in the frame.
func TestProsePopupMaximizedUsesReadingColumn(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.width, m.height = 240, 40
	long := strings.Repeat("lorem ipsum dolor sit amet ", 30)
	cp := newContentPopup("Review: x", []contentLine{{text: long}})
	cp.mode, cp.noCursor, cp.prose = modeWrap, true, true
	cp.maximized = true
	lo, hi := textSpan(t, cp.box(m))
	if hi-lo > 120 {
		t.Fatalf("text spans columns %d..%d (%d wide), want at most the 120-column reading width", lo, hi, hi-lo)
	}
	if lo < 50 {
		t.Fatalf("text starts at column %d: the reading column is not centred", lo)
	}
	// Not prose: the same popup still uses the whole frame.
	cp.prose = false
	if lo, hi := textSpan(t, cp.box(m)); hi-lo <= 120 {
		t.Fatalf("a non-prose popup must keep the full width, spans %d..%d", lo, hi)
	}
}

// View all notes wraps a long NOTE under its own column instead of cutting it,
// and a maximized popup centres the table in the reading column.
func TestAllNotesWrapsNoteColumn(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.width, m.height = 240, 40
	ov := allNotesFixture()
	long := strings.Repeat("the summary keeps going ", 12) + "END"
	ov.Unstaged[0].Notes[0].Note.Summary = long
	p := &allNotesPopup{rows: buildAllNotesRows(ov, "repo"), folded: map[string]bool{}}
	p.maximized = true
	box := plain(p.box(m))
	if !strings.Contains(box, "END") {
		t.Fatalf("the long summary was cut, not wrapped:\n%s", box)
	}
	lo, hi := textSpan(t, box)
	if hi-lo > 120 || lo < 50 {
		t.Fatalf("table spans %d..%d; want ≤120 wide and centred:\n%s", lo, hi, box)
	}
	// The continuation hangs under the NOTE column: find the row after the
	// summary's first line and check it starts where the summary started.
	lines := strings.Split(box, "\n")
	for i, l := range lines {
		at := strings.Index(l, "the summary keeps")
		if at < 0 || i+1 >= len(lines) {
			continue
		}
		next := lines[i+1]
		cont := strings.TrimLeft(next[:min(len(next), at)], " │║")
		if cont != "" || len(next) <= at || next[at] == ' ' {
			t.Fatalf("continuation %q does not hang under the NOTE column (col %d)", next, at)
		}
		return
	}
	t.Fatalf("summary not found:\n%s", box)
}
