package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// The SPLIT layer (spec 2026-10-10-review-overview-split §1): two panes side
// by side, one focused. The split owns the keys that move between the sides
// and the one that closes it; every other key goes to the focused pane. It
// is a generic full-screen surface — the review's overview beside its files
// is its first use — and knows nothing about what a pane shows.

// pane is one side of a split: a surface with its own content, cursor and
// keys, drawn as a bordered box w×h. captures says the pane must see a key
// before the split reads it (a search being typed takes every key, a live
// line selection takes esc). doc is the document shown (nil = none), for
// the seams that ask "which document is the keyboard on".
type pane interface {
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	render(m Model, w, h int, focused bool) string
	title() string
	hint(m Model) string
	captures(msg tea.KeyMsg) bool
	doc() *openFile
}

// splitView is the layer: the two panes, the focused side, and what esc
// does once the split is gone (nil = nothing).
type splitView struct {
	left, right pane
	onLeft      bool
	onClose     func(Model) Model
}

// splitMinWidth is the narrowest terminal a split is opened on: two
// 30-column boxes. Narrower, callers open their full-screen fallback.
const splitMinWidth = 60

// splitGeom is the two boxes' sizes: the left half (rounded down), the rest
// on the right, both one row short of the screen (the hint line).
func (m Model) splitGeom() (leftW, rightW, boxH int) {
	w, h := m.overlayDims()
	leftW = w / 2
	rightW = w - leftW
	boxH = h - 1
	if boxH < 4 {
		boxH = 4
	}
	return leftW, rightW, boxH
}

// paneGeom is a box's content size: the preview box's math (border 2,
// padding 2, a title row) with no hint row of its own.
func paneGeom(boxW, boxH int) (rows, innerW int) {
	rows, innerW = boxH-3, boxW-4
	if rows < 1 {
		rows = 1
	}
	if innerW < 1 {
		innerW = 1
	}
	return rows, innerW
}

// focused is the pane the keyboard is on.
func (sv *splitView) focused() pane {
	if sv.onLeft {
		return sv.left
	}
	return sv.right
}

// geomOf is pane p's content size in this split.
func (sv *splitView) geomOf(m Model, p pane) (rows, innerW int) {
	lw, rw, bh := m.splitGeom()
	if p == sv.left {
		return paneGeom(lw, bh)
	}
	return paneGeom(rw, bh)
}

// holds reports whether either pane shows d.
func (sv *splitView) holds(d *openFile) bool { return sv.paneOf(d) != nil }

// paneOf is the pane showing d, or nil.
func (sv *splitView) paneOf(d *openFile) pane {
	if d == nil {
		return nil
	}
	if sv.left.doc() == d {
		return sv.left
	}
	if sv.right.doc() == d {
		return sv.right
	}
	return nil
}

func (sv *splitView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	f := sv.focused()
	if f.captures(msg) {
		return f.update(m, msg)
	}
	switch msg.String() {
	case "tab", "shift+tab":
		sv.onLeft = !sv.onLeft
		return m, nil
	case "alt+left":
		sv.onLeft = true
		return m, nil
	case "alt+right":
		sv.onLeft = false
		return m, nil
	case "backspace":
		sv.onLeft = true
		return m, nil
	case "esc":
		return m.closeSplit(sv), nil
	case "N", "P":
		// The single file pane: the overview's next / previous anchor, as
		// ↑ / ↓ on the overview, the focus kept right. (The stacked pane's
		// N / P walk its file headers.)
		if _, single := f.(*docPane); single && !sv.onLeft {
			dir := 1
			if msg.String() == "P" {
				dir = -1
			}
			return m.stepOverviewAnchor(sv, dir)
		}
	}
	return f.update(m, msg)
}

// closeSplit pops sv and runs its onClose: esc, and every other way the
// split is left on purpose (X on the overview, backgrounding the overview).
func (m Model) closeSplit(sv *splitView) Model {
	m = m.removeLayer(sv)
	if sv.onClose != nil {
		m = sv.onClose(m)
	}
	return m
}

// dropSplits takes every split off the stack, its panes' documents left
// open in the background: the files view a split was opened from is closing
// (a narrow resize, a deleted review, another view replacing it), so there
// is nothing for the split to steer through or return to.
func (m Model) dropSplits() Model {
	for sv := m.splitLayer(); sv != nil; sv = m.splitLayer() {
		for _, p := range []pane{sv.left, sv.right} {
			if d := p.doc(); d != nil {
				d.backgrounded = true
			}
		}
		m = m.removeLayer(sv)
	}
	return m
}

// render owns the screen: the two boxes joined, one hint line under them —
// the focused pane's keys, then the split's own. A status message leads the
// hint (the split covers the status bar), as the viewer's title line does.
func (sv *splitView) render(m Model, _ string) string {
	lw, rw, bh := m.splitGeom()
	w, _ := m.overlayDims()
	boxes := lipgloss.JoinHorizontal(lipgloss.Top, sv.left.render(m, lw, bh, sv.onLeft), sv.right.render(m, rw, bh, !sv.onLeft))
	// The way between the sides and the exit lead: the hint is cut at the
	// screen's width, and the pane's own keys teach themselves by doing.
	hint := i18n.T("[tab] other side  [bksp] left side  [esc] close")
	if h := sv.focused().hint(m); h != "" {
		hint += "  " + h
	}
	if m.statusMsg != "" && !m.running && m.statusMsg != m.stickyMsg {
		hint = m.statusMsg + "  " + hint
	}
	return strings.TrimRight(boxes, "\n") + "\n" + padRight(truncate(hint, w), w)
}

// splitLayer is the topmost split on the stack, or nil.
func (m Model) splitLayer() *splitView { return layerOf[*splitView](m) }

// splitFocus answers "which document is the keyboard on" for a split ON
// TOP of the stack whose focused pane shows a document: that document and
// its content size. A split under a popup is not on the keyboard.
func (m Model) splitFocus() (sv *splitView, d *openFile, rows, innerW int, ok bool) {
	sv, isSplit := m.topLayer().(*splitView)
	if !isSplit {
		return nil, nil, 0, 0, false
	}
	p := sv.focused()
	d = p.doc()
	if d == nil {
		return sv, nil, 0, 0, false
	}
	rows, innerW = sv.geomOf(m, p)
	return sv, d, rows, innerW, true
}

// splitShowing is the split anywhere on the stack showing d, or nil.
func (m Model) splitShowing(d *openFile) *splitView {
	if m.layers == nil || d == nil {
		return nil
	}
	for i := len(m.layers.entries) - 1; i >= 0; i-- {
		if sv, ok := m.layers.entries[i].(*splitView); ok && sv.holds(d) {
			return sv
		}
	}
	return nil
}

// splitMouse is a press over split sv: the wheel scrolls the side under the
// pointer without moving the focus; a left click focuses that side, then
// selects (and steers) an anchor of an overview or puts a file's cursor on
// the row clicked; a right click opens the . menu of the side it lands on;
// a middle click is esc (it closes the split, as on every reader).
func (m Model) splitMouse(sv *splitView, msg tea.MouseMsg, wheel int) (Model, tea.Cmd) {
	lw, _, _ := m.splitGeom()
	side, x := sv.right, msg.X-lw
	if msg.X < lw {
		side, x = sv.left, msg.X
	}
	rows, _ := sv.geomOf(m, side)
	d := side.doc()
	if wheel != 0 {
		if d != nil {
			d.p.scrollBy(wheel, rows)
		}
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		sv.onLeft = side == sv.left
		if d == nil {
			return m, nil
		}
		if d.ov != nil {
			return m.overviewClickIn(d, sv, x, msg.Y, rows, 0)
		}
		if row := msg.Y - 2; row >= 0 && row < rows {
			if line := d.p.clampTop(d.p.sel, rows) + row; line < len(d.p.lines) {
				d.p.cur = line
			}
		}
		return m, nil
	case tea.MouseButtonRight:
		sv.onLeft = side == sv.left
		return sv.update(m, synthKey("."))
	case tea.MouseButtonMiddle:
		return sv.update(m, synthKey("esc"))
	}
	return m, nil
}
