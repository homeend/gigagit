package tui

import "testing"

// alt+↓ inside another anchor's band marks that anchor on the overview and
// leaves the selection alone (spec 2026-10-10-overview-split-all R2).
func TestSplitRightCursorInAnotherBandMarksNotSelects(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t) // a.go:3 selected; "A again" (a.go:1-3) is another band in a.go
	ov := sv.left.doc()
	m = pressDrain(t, m, "tab")
	sv.right.doc().p.cur = 0         // line 1: only the a.go:1-3 band covers it
	m = pressDrain(t, m, "alt+down") // line 2, still a.go:1-3 only
	if ov.ov.sel != 0 {
		t.Fatalf("sel %d: the right side never moves the selection", ov.ov.sel)
	}
	if ov.ov.here() != 4 {
		t.Fatalf("here %d, want 4 (A again)", ov.ov.here())
	}
	_ = m
}

// N / P name the overview: they move the selection, and the mark clears
// once the cursor sits in the selected anchor's own band.
func TestSplitNPCapitalStillSelects(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	m = pressDrain(t, m, "tab")
	pressDrain(t, m, "N")
	if ov.ov.sel != 1 || ov.ov.here() != -1 {
		t.Fatalf("sel %d here %d", ov.ov.sel, ov.ov.here())
	}
}

// The mark paints its own class; the selection keeps reverse video.
func TestOverviewPaintsHereApartFromSel(t *testing.T) {
	t.Parallel()
	_, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	ov.ov.mark = 2
	ov.ov.paint(ov.p.lines)
	s := ov.ov.anchors[1].spans[0]
	if c := ov.p.lines[s.line].cls[s.from]; c != mdAnchorHere {
		t.Fatalf("class %v, want mdAnchorHere", c)
	}
	s0 := ov.ov.anchors[0].spans[0]
	if c := ov.p.lines[s0.line].cls[s0.from]; c != mdAnchorSel {
		t.Fatalf("selected class %v", c)
	}
}

// A re-layout (a new text, a width) forgets the mark: its index may name
// another anchor now.
func TestOverviewLayOutClearsHere(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	ov.ov.mark = 5
	rows, inner := sv.geomOf(m, sv.left)
	ov.layOut(rows, inner-1)
	if ov.ov.here() != -1 {
		t.Fatalf("here %d after a re-layout", ov.ov.here())
	}
}
