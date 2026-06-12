package tui

// layoutGeom is the panel geometry renderInterface draws with. boxH holds each
// panel's box height under the current layout; a panel missing from the map
// (or 0) is not visible at this terminal size.
type layoutGeom struct {
	w, h, bodyH   int
	leftW, rightW int
	boxH          map[panel]int
}

// layout computes panel geometry for the current terminal size. It is the
// single source of truth shared by renderInterface and the paging keys, so
// rendering and navigation can never disagree about a panel's viewport.
func (m Model) layout() layoutGeom {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	bodyH := h - 3
	if bodyH < 6 {
		bodyH = 6
	}
	g := layoutGeom{w: w, h: h, bodyH: bodyH, boxH: map[panel]int{}}

	// Narrow terminals: a single commits column.
	if w < 40 {
		g.rightW = w
		g.boxH[panelCommits] = bodyH
		return g
	}

	leftW := w / 3
	if leftW < 16 {
		leftW = 16
	}
	if leftW > w-24 {
		leftW = w - 24
	}
	g.leftW, g.rightW = leftW, w-leftW

	if bodyH >= 9 {
		// Three stacked left panels (each bordered panel needs >=3 rows).
		h1 := bodyH / 3
		h2 := bodyH / 3
		g.boxH[panelBranches] = h1
		g.boxH[panelWorktrees] = h2
		g.boxH[panelStatus] = bodyH - h1 - h2
	} else {
		// Short terminal: Branches over Status only.
		bh := bodyH / 2
		g.boxH[panelBranches] = bh
		g.boxH[panelStatus] = bodyH - bh
	}
	g.boxH[panelCommits] = bodyH
	return g
}

// panelRowsCap is how many data rows panel p can display right now (0 when the
// layout hides it). Mirrors renderPanel: box height minus borders (2) minus
// the label line (1).
func (m Model) panelRowsCap(p panel) int {
	n := m.layout().boxH[p] - 3
	if n < 0 {
		n = 0
	}
	return n
}

// pageStep is the pgup/pgdown jump: 25% of the focused panel's viewport,
// at least 1 row.
func (m Model) pageStep() int {
	s := m.panelRowsCap(m.focus) / 4
	if s < 1 {
		s = 1
	}
	return s
}
