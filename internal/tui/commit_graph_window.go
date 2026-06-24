package tui

import "github.com/homeend/gigagit/internal/commitgraph"

// graphActive reports whether the windowed lane graph is currently drawn in the
// Commits panel (natural order, not list mode, cells cached and aligned).
func (m Model) graphActive() bool {
	return !m.commitListMode && m.commitGraphOn() &&
		len(m.commitGraphRows) == m.commitsTotal() && m.commitsTotal() > 0
}

// graphDefaultLanes is the configured startup window width ([ui]
// commit_graph_lanes), 8 until config loads.
func (m Model) graphDefaultLanes() int {
	if v := m.cfg.UI.CommitGraphLanes; v > 0 {
		return v
	}
	return 8
}

// graphMinLanes is the narrow floor ([ui] commit_graph_min_lanes), 2 by default.
func (m Model) graphMinLanes() int {
	if v := m.cfg.UI.CommitGraphMinLanes; v > 0 {
		return v
	}
	return 2
}

// graphStep is the widen/narrow increment in lanes ([ui] commit_graph_step), 4
// by default.
func (m Model) graphStep() int {
	if v := m.cfg.UI.CommitGraphStep; v > 0 {
		return v
	}
	return 4
}

// graphPanStep is the pan increment in lanes ([ui] commit_graph_pan_step); when
// unset it is derived as half the current window (min 1) for a half-page feel.
func (m Model) graphPanStep() int {
	if v := m.cfg.UI.CommitGraphPanStep; v > 0 {
		return v
	}
	if s := m.graphCols() / 2; s > 0 {
		return s
	}
	return 1
}

// graphMaxLanes is the effective plane cap: the configured value clamped to the
// hard code ceiling commitgraph.MaxLanes (config can only lower it).
func (m Model) graphMaxLanes() int {
	cap := commitgraph.MaxLanes
	if c := m.cfg.UI.CommitGraphMaxLanes; c > 0 && c < cap {
		cap = c
	}
	return cap
}

// graphPlaneLanes is the true lane width of the cached graph (all rows share the
// padded Width), bounded by the effective cap.
func (m Model) graphPlaneLanes() int {
	if len(m.commitGraphRows) == 0 {
		return 0
	}
	w := len([]rune(m.commitGraphRows[0])) / 2
	if cap := m.graphMaxLanes(); w > cap {
		w = cap
	}
	return w
}

// graphCols resolves the current window width in lanes: the stored preference
// (0 = configured default), floored at min and capped at the plane width.
func (m Model) graphCols() int {
	cols := m.commitGraphCols
	if cols <= 0 {
		cols = m.graphDefaultLanes()
	}
	return m.clampCols(cols)
}

// clampCols floors c at the configured min and caps it at the plane width.
func (m Model) clampCols(c int) int {
	if mn := m.graphMinLanes(); c < mn {
		c = mn
	}
	if pl := m.graphPlaneLanes(); pl > 0 && c > pl {
		c = pl
	}
	return c
}

// clampScroll keeps the horizontal offset within [0, planeLanes-cols].
func (m Model) clampScroll(s int) int {
	if s < 0 {
		return 0
	}
	max := m.graphPlaneLanes() - m.graphCols()
	if max < 0 {
		max = 0
	}
	if s > max {
		s = max
	}
	return s
}

// graphWindow slices the cached full graph cells to the current horizontal
// window [scroll, scroll+cols) lanes, pads to cols*2 columns, and reports
// whether content exists beyond each edge. A ⋯ marker replaces the edge column
// when there is more content past it. Rune-aware (cells hold 3-byte glyphs).
func (m Model) graphWindow(cells string) (visible string, leftMore, rightMore bool) {
	cols := m.graphCols()
	scroll := m.commitGraphScroll
	r := []rune(cells)
	start := scroll * 2
	end := start + cols*2

	for i := end; i < len(r); i++ {
		if r[i] != ' ' {
			rightMore = true
			break
		}
	}

	var win []rune
	if start < len(r) {
		e := end
		if e > len(r) {
			e = len(r)
		}
		win = append(win, r[start:e]...)
	}
	for len(win) < cols*2 {
		win = append(win, ' ')
	}

	leftMore = scroll > 0
	if leftMore && len(win) > 0 {
		win[0] = '⋯'
	}
	if rightMore && len(win) > 0 {
		win[len(win)-1] = '⋯'
	}
	return string(win), leftMore, rightMore
}

// snapGraphToSelected scrolls the graph window so the selected commit's node
// lane is centered-ish in view.
func (m Model) snapGraphToSelected() Model {
	u := m.commitSelUnified() // graph caches span the unified WIP+feed space
	if u < 0 || u >= len(m.commitGraphLanes) {
		return m
	}
	lane := m.commitGraphLanes[u]
	m.commitGraphScroll = m.clampScroll(lane - m.graphCols()/2)
	return m
}
