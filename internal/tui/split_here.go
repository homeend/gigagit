package tui

// syncOverviewHere marks on the overview the anchor whose band the right
// pane's cursor sits in (spec 2026-10-10-overview-split-all §7): the current
// band first, else the first band covering the line. None, or the selected
// anchor itself, clears the mark. Nothing on the left moves.
func (m Model) syncOverviewHere(sv *splitView) {
	ov := sv.left.doc()
	if ov == nil || ov.ov == nil {
		return
	}
	here := -1
	if d := sv.right.doc(); d != nil && d.from == ov {
		bs := d.bands()
		line, cur := d.p.cur+1, d.curBand(bs)
		if cur >= 0 && bs[cur].start <= line && line <= bs[cur].end {
			here = bs[cur].i
		} else {
			for _, b := range bs {
				if b.start <= line && line <= b.end {
					here = b.i
					break
				}
			}
		}
	}
	if here == ov.ov.sel || here >= len(ov.ov.anchors) {
		here = -1
	}
	if here != ov.ov.here() {
		ov.ov.mark = here + 1
		ov.ov.paint(ov.p.lines)
	}
}
