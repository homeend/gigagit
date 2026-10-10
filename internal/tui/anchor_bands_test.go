package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/theme"
)

func TestAnchorBandStylesFollowTheTheme(t *testing.T) {
	t.Parallel()
	s := buildStyles(theme.Dark)
	if _, none := s.anchorBand.GetBackground().(lipgloss.NoColor); none {
		t.Fatal("dark theme: the other anchors' band has no background")
	}
	if _, none := s.anchorBandCur.GetBackground().(lipgloss.NoColor); none {
		t.Fatal("dark theme: the current anchor's band has no background")
	}
	if s.anchorBand.GetBackground() == s.anchorBandCur.GetBackground() {
		t.Fatal("dark theme: current and other bands share a colour")
	}
	s = buildStyles(theme.Terminal)
	if _, none := s.anchorBand.GetBackground().(lipgloss.NoColor); !none {
		t.Fatal("terminal theme: an anchor band paints a background")
	}
}

func band(start, end, i int) anchorBand { return anchorBand{start, end, i} }

func anc(dest, path string, start, end int) anchor {
	return anchor{dest: dest, target: agentdocs.Anchor{Dest: dest, Path: path, Start: start, End: end}}
}

func TestAnchorBandsFilterAndKinds(t *testing.T) {
	t.Parallel()
	as := []anchor{
		anc("a.go", "a.go", 0, 0),     // the file: no line, no band
		anc("a.go:12", "a.go", 12, 0), // a line: a one-line band
		anc("a.go:5-8", "a.go", 5, 8), // a range
		anc("b.go:3", "b.go", 3, 0),   // another file
		{dest: "note:t1", target: agentdocs.Anchor{Note: "t1", Path: "a.go", Start: 20, End: 20}}, // a note: never
	}
	got := anchorBands(as, "a.go", 100)
	want := []anchorBand{band(5, 8, 2), band(12, 12, 1)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

// A plain anchor (a stored overview's range the review cannot open, R3)
// gets no band and is never the current band: n/p must not land on it,
// and a resolved anchor on the same lines keeps its own band.
func TestAnchorBandsSkipPlainAnchors(t *testing.T) {
	t.Parallel()
	plain := anc("x:40-60", "x", 40, 60)
	plain.plain = true
	twin := anc("x:5-8", "x", 5, 8)
	twin.plain = true
	as := []anchor{plain, twin, anc("x:5-8", "x", 5, 8), anc("x:12", "x", 12, 0)}
	got := anchorBands(as, "x", 100)
	want := []anchorBand{band(5, 8, 2), band(12, 12, 3)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if k := bandOf(got, as, "x:40-60", 100); k != -1 {
		t.Fatalf("a plain anchor has band %d", k)
	}
}

func TestAnchorBandsDedupeAndOrder(t *testing.T) {
	t.Parallel()
	as := []anchor{anc("x:9-10", "x", 9, 10), anc("x:2", "x", 2, 0), anc("./x:9-10", "x", 9, 10), anc("x:2-4", "x", 2, 4)}
	got := anchorBands(as, "x", 50)
	want := []anchorBand{band(2, 2, 1), band(2, 4, 3), band(9, 10, 0)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestAnchorBandsDropAndClamp(t *testing.T) {
	t.Parallel()
	as := []anchor{anc("x:8-30", "x", 8, 30), anc("x:31", "x", 31, 0), anc("x:10", "x", 10, 0)}
	got := anchorBands(as, "x", 20)
	want := []anchorBand{band(8, 20, 0), band(10, 10, 2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if anchorBands(as, "x", 0) != nil {
		t.Fatal("an empty file has bands")
	}
}

func TestBandOf(t *testing.T) {
	t.Parallel()
	as := []anchor{anc("x:9-10", "x", 9, 10), anc("x:2", "x", 2, 0), anc("./x:9-10", "x", 9, 10)}
	bs := anchorBands(as, "x", 50)
	if bandOf(bs, as, "./x:9-10", 50) != 1 || bandOf(bs, as, "x:2", 50) != 0 || bandOf(bs, as, "gone", 50) != -1 || bandOf(bs, as, "", 50) != -1 {
		t.Fatal("bandOf")
	}
	// same start, different end: the exact band, never the first sharing a start
	as = []anchor{anc("x:2", "x", 2, 0), anc("x:2-4", "x", 2, 4), anc("x:8-30", "x", 8, 30)}
	bs = anchorBands(as, "x", 20)
	if bandOf(bs, as, "x:2-4", 20) != 1 || bandOf(bs, as, "x:8-30", 20) != 2 {
		t.Fatalf("bandOf exact: %d %d", bandOf(bs, as, "x:2-4", 20), bandOf(bs, as, "x:8-30", 20))
	}
}

func TestStepBand(t *testing.T) {
	t.Parallel()
	bs := []anchorBand{band(5, 8, 0), band(12, 12, 1), band(30, 31, 2)}
	cases := []struct {
		cur, line, dir, want int
		wrapped              bool
	}{
		{1, 12, 1, 2, false}, // from the current band
		{2, 30, 1, 0, true},  // wraps forward
		{0, 6, -1, 2, true},  // wraps back from inside the current band
		{-1, 1, 1, 0, false}, // no current: from the cursor
		{-1, 40, 1, 0, true}, // past every band: wraps
		{-1, 20, -1, 1, false},
		{-1, 3, -1, 2, true},
		{1, 25, 1, 2, false}, // the cursor walked off the current band: from the cursor
		{1, 25, -1, 1, false},
	}
	for _, c := range cases {
		got, w := stepBand(bs, c.cur, c.line, c.dir)
		if got != c.want || w != c.wrapped {
			t.Errorf("stepBand(cur=%d line=%d dir=%d) = %d,%v want %d,%v", c.cur, c.line, c.dir, got, w, c.want, c.wrapped)
		}
	}
	if got, w := stepBand(bs[:1], 0, 5, 1); got != 0 || w {
		t.Fatalf("one band: %d,%v want 0,false", got, w)
	}
	if got, _ := stepBand(nil, -1, 5, 1); got != -1 {
		t.Fatal("no bands")
	}
}

func TestStepBandOverlap(t *testing.T) {
	t.Parallel()
	bs := []anchorBand{band(2, 2, 0), band(2, 4, 1), band(3, 9, 2)}
	var seen []int
	cur := 0
	for range 3 {
		cur, _ = stepBand(bs, cur, bs[cur].start, 1)
		seen = append(seen, cur)
	}
	if !reflect.DeepEqual(seen, []int{1, 2, 0}) {
		t.Fatalf("walk = %v, want every band once", seen)
	}
}

func TestGutterMarkPrecedence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		k     bandKind
		noted bool
		want  string
	}{
		{bandCurrent, true, "┃ "}, {bandOther, true, "╎ "}, {bandNone, true, "│ "}, {bandNone, false, ""},
	}
	for _, c := range cases {
		if got := gutterMark(c.k, c.noted); got != c.want {
			t.Errorf("gutterMark(%d,%v) = %q want %q", c.k, c.noted, got, c.want)
		}
	}
}

func TestBandKindAtCurrentWinsAnOverlap(t *testing.T) {
	t.Parallel()
	bs := []anchorBand{band(2, 6, 0), band(4, 4, 1)}
	if bandKindAt(bs, 1, 4) != bandCurrent || bandKindAt(bs, 1, 3) != bandOther || bandKindAt(bs, -1, 4) != bandOther || bandKindAt(bs, 1, 7) != bandNone {
		t.Fatal("bandKindAt")
	}
}

func TestBandedFileDrawsItsGutter(t *testing.T) {
	t.Parallel()
	m, d, _ := tourModel(t)
	m = openNth(t, m, d, 1) // a.txt:12 current, 5-8 other
	f := topDoc(m)
	// Count, not Contains: a frame border may itself be drawn with ┃.
	withCur := strings.Count(m.View(), "┃ ")
	f.anchorCur = "" // line 12 becomes an "other" band
	v := m.View()
	if withCur-strings.Count(v, "┃ ") != 1 || strings.Count(v, "╎ ") < 5 {
		t.Fatalf("band gutter wrong (┃ diff %d):\n%s", withCur-strings.Count(v, "┃ "), v)
	}
}

func TestPreviewRowMarkLaysTheBandUnderTheCursor(t *testing.T) {
	t.Parallel()
	p := &contentPopup{lines: []contentLine{{text: "a"}, {text: "b"}}, cur: 0}
	if _, marked := previewRowMark(p, 1, false, p.lines[1], bandNone); marked {
		t.Fatal("a plain row is marked")
	}
	if _, marked := previewRowMark(p, 1, false, p.lines[1], bandOther); !marked {
		t.Fatal("a banded row is not marked")
	}
	cur, _ := previewRowMark(p, 0, false, p.lines[0], bandCurrent)
	if cur.GetBackground() != st().diffCursorRow.GetBackground() {
		t.Fatal("the cursor row lost to the band")
	}
}
