package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/homeend/gigagit/internal/textdiff"
)

// revealView is a diff of one very long line holding two far-apart hits,
// searched for "needle", inside a model of the given size.
func revealView(t *testing.T, long longMode, text string) (Model, *diffView, int) {
	t.Helper()
	rows := []textdiff.Row{{Kind: textdiff.Same, Left: text, Right: text, LeftNo: 1, RightNo: 1}}
	m := diffModel()
	m.width, m.height = 100, 24
	v := diffViewWith(rows, nil)
	v.long = long
	v.width = m.width
	v.rebuild()
	m = m.pushLayer(v)
	body := m.diffBodyRows()
	v.search.query = "needle"
	v.search.refindFrom(v.searchLines(), v.searchPos())
	if len(v.search.hits) == 0 {
		t.Fatal("fixture: no hits")
	}
	return m, v, body
}

// hitShown: some body row shows the whole word — not the search bar's
// "/needle", and not cut by a pan marker (‹needl… or …eedle›).
func hitShown(m Model) bool {
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "needle") && !strings.Contains(l, "/needle") {
			return true
		}
	}
	return false
}

// Scroll mode pans to a hit far to the right — and the hit must clear the
// ‹ › pan markers, which take the window's edge columns: the hit used to end
// exactly at the edge, its last letter under the › (stepping right) or its
// first under the ‹ (stepping back left).
func TestDiffScrollRevealKeepsTheHitClearOfThePanMarkers(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("x", 300) + "needle" + strings.Repeat("y", 400) + "needle" + strings.Repeat("z", 300)
	m, v, body := revealView(t, longScroll, text)
	// each hit, reached from either side (the 2nd hit from the 1st steps
	// right, the 1st from the 2nd steps left)
	for _, step := range []int{0, 1, 0} {
		v.goToHit(step, body) // hits are in line order
		if v.search.cur != step {
			t.Fatalf("fixture: cur %d, want %d", v.search.cur, step)
		}
		if !hitShown(m) {
			t.Fatalf("hit %d (hOffset %d) is not wholly on screen:\n%s", step, v.hOffset, ansi.Strip(m.View()))
		}
	}
}

// Wrap mode: a long line spans many display rows; the hit is on one of them,
// screens below the line's first. The view must scroll to THAT row, not
// stop at the line's start.
func TestDiffWrapRevealScrollsToTheHitsRow(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("word ", 1200) + "needle " + strings.Repeat("tail ", 200)
	m, v, body := revealView(t, longWrap, text)
	v.goToHit(0, body)
	if !hitShown(m) {
		t.Fatalf("wrapped hit not on screen (offset %d, body %d):\n%s", v.offset, body, ansi.Strip(m.View()))
	}
}

func TestPanForMarked(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name                      string
		h, tw, cs, ce, full, want int
	}{
		{"on screen between the markers: no pan", 10, 20, 12, 16, 100, 10},
		{"under the ‹: past it", 10, 20, 11, 15, 100, 10},
		{"first column under the ‹ moves one left", 10, 20, 10, 14, 100, 9},
		{"left of the window: just past the ‹", 50, 20, 30, 34, 100, 29},
		{"near the start: the line's own first column, no ‹", 50, 20, 1, 4, 100, 0},
		{"to the right: one short of the ›", 0, 20, 40, 46, 100, 27},
		{"at the line's end there is no ›", 0, 20, 94, 100, 100, 80},
		{"wider than the window: its head", 0, 20, 40, 80, 100, 39},
		{"already shown at rest", 0, 20, 2, 6, 100, 0},
	} {
		if got := panForMarked(c.h, c.tw, c.cs, c.ce, c.full); got != c.want {
			t.Errorf("%s: panForMarked(%d, %d, %d, %d, %d) = %d, want %d", c.name, c.h, c.tw, c.cs, c.ce, c.full, got, c.want)
		}
	}
}
