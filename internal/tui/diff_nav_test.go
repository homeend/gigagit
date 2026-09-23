package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/textdiff"
)

// A free scroll (arrows, wheel) moves the viewport and NOT the cursor, so the
// focused change can end up off screen. n then stepped from that invisible
// change: with one change in the file it only primed the wrap, and the reader
// had to press n twice to be taken back to a change they could not see. The
// step must start from what is ON SCREEN — gg web's visibleChangeBlock rule.
func TestNextChangeStartsFromTheViewportWhenTheFocusScrolledAway(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(200, 100) // one change, deep in a long file
	v := diffViewWith(rows, blocksOf(rows))
	m := diffModel()
	m.height, m.width = 24, 120
	m = m.pushLayer(v)
	body := m.diffBodyRows()
	v.focusBlock(0, body) // where a fresh diff opens

	// the reader scrolls back to the top; the change is far below the pane
	v.offset = 0
	v.scroll(0, body)
	if v.dispBlocks[v.cur] < v.offset+body {
		t.Fatal("fixture: the focused change must be off screen for this test to mean anything")
	}

	u, _ := m.Update(keyMsg("n"))
	nm := u.(Model)
	nv := nm.diffLayer()
	if nv.wrapArm != wrapNone {
		t.Fatal("n primed a wrap instead of stepping to the change below the viewport")
	}
	if nv.dispBlocks[nv.cur] < nv.offset || nv.dispBlocks[nv.cur] >= nv.offset+nm.diffBodyRows() {
		t.Fatalf("after one n the change is still off screen (block row %d, viewport %d–%d)",
			nv.dispBlocks[nv.cur], nv.offset, nv.offset+nm.diffBodyRows())
	}
}

// …and p is the mirror: scrolled BELOW the change, one p brings back the
// change above the viewport rather than priming the backwards wrap.
func TestPrevChangeStartsFromTheViewportWhenTheFocusScrolledAway(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(200, 20) // the change is near the top
	v := diffViewWith(rows, blocksOf(rows))
	m := diffModel()
	m.height, m.width = 24, 120
	m = m.pushLayer(v)
	body := m.diffBodyRows()
	v.focusBlock(0, body)

	v.offset = len(v.disp) - body // scroll to the end: the change is above the pane
	v.scroll(0, body)
	if v.dispBlocks[v.cur] >= v.offset {
		t.Fatal("fixture: the focused change must be above the viewport")
	}

	u, _ := m.Update(keyMsg("p"))
	nv := u.(Model).diffLayer()
	if nv.wrapArm != wrapNone {
		t.Fatal("p primed a wrap instead of stepping to the change above the viewport")
	}
	if nv.dispBlocks[nv.cur] < nv.offset || nv.dispBlocks[nv.cur] >= nv.offset+body {
		t.Fatalf("after one p the change is still off screen (block row %d, viewport %d–%d)",
			nv.dispBlocks[nv.cur], nv.offset, nv.offset+body)
	}
}

// With the focused change ON screen nothing changes: n steps to the next one,
// and at the last change it still primes the wrap (the shipped rule).
func TestNextChangeKeepsSteppingWhenTheFocusIsVisible(t *testing.T) {
	t.Parallel()
	rows := sameRowsTUI(40, 5, 12, 19)
	v := diffViewWith(rows, blocksOf(rows))
	m := diffModel()
	m.height, m.width = 40, 120
	m = m.pushLayer(v)
	body := m.diffBodyRows()
	v.focusBlock(0, body)

	u, _ := m.Update(keyMsg("n"))
	if got := u.(Model).diffLayer().cur; got != 1 {
		t.Fatalf("n moved to change %d, want the next one (1)", got)
	}
	m2 := u.(Model)
	m2.diffLayer().focusBlock(len(v.dispBlocks)-1, m2.diffBodyRows())
	u2, _ := m2.Update(keyMsg("n"))
	if u2.(Model).diffLayer().wrapArm != wrapToStart {
		t.Fatal("n on the last visible change must still prime the wrap")
	}
}

var _ = textdiff.Same
