package tui

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// Overview anchor bands (spec 2026-10-07-overview-anchor-bands): a file an
// overview's anchor opened draws every line / range anchor that overview
// has in it — the current one apart from the rest — and n / p walk them.

// anchorBand is one band: lines start..end (1-based) of the file, standing
// for overview anchor i (the first in document order with that range).
type anchorBand struct{ start, end, i int }

// anchorBands is the bands anchors make in path, a file of nLines lines: its
// line and range anchors (a file anchor has no line; a note anchor is the
// note's own mark), sorted by start then end, one per range, a band past the
// last line dropped and one running past it cut there.
func anchorBands(anchors []anchor, path string, nLines int) []anchorBand {
	var out []anchorBand
	seen := map[[2]int]bool{}
	for i, a := range anchors {
		t := a.target
		if t.Note != "" || t.Path != path || t.Start <= 0 || t.Start > nLines {
			continue
		}
		end := min(max(t.End, t.Start), nLines)
		k := [2]int{t.Start, end}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, anchorBand{t.Start, end, i})
	}
	sort.SliceStable(out, func(x, y int) bool {
		if out[x].start != out[y].start {
			return out[x].start < out[y].start
		}
		return out[x].end < out[y].end
	})
	return out
}

// bandOf is the index in bands of the band anchor dest makes in a file of
// nLines lines (its range clamped as anchorBands clamps it), or -1.
func bandOf(bands []anchorBand, anchors []anchor, dest string, nLines int) int {
	if dest == "" {
		return -1
	}
	for _, a := range anchors {
		t := a.target
		if a.dest != dest || t.Note != "" || t.Start <= 0 {
			continue
		}
		end := min(max(t.End, t.Start), nLines)
		for k, b := range bands {
			if b.start == t.Start && b.end == end {
				return k
			}
		}
	}
	return -1
}

// stepBand is n (dir 1) / p (-1): from band cur when the cursor line is in
// it, else from the cursor — the next band starting below it, the last
// starting above it — wrapping at the ends (wrapped says so; one band never
// "wraps"). -1 when there are none.
func stepBand(bands []anchorBand, cur, line, dir int) (int, bool) {
	n := len(bands)
	if n == 0 {
		return -1, false
	}
	if cur >= 0 && cur < n && bands[cur].start <= line && line <= bands[cur].end {
		next := cur + dir
		return ((next % n) + n) % n, (next < 0 || next >= n) && n > 1
	}
	if dir > 0 {
		for k, b := range bands {
			if b.start > line {
				return k, false
			}
		}
		return 0, n > 1
	}
	for k := n - 1; k >= 0; k-- {
		if bands[k].start < line {
			return k, false
		}
	}
	return n - 1, n > 1
}

// bands is the bands of the overview that opened d, while it is open; nil
// for any other document.
func (d *openFile) bands() []anchorBand {
	if d.from == nil || d.from.ov == nil || d.from.ov.closed || !docLoaded(d) || d.p.img != nil {
		return nil
	}
	return anchorBands(d.from.ov.anchors, d.path, len(d.p.lines))
}

// curBand is the index in bands of d's current anchor, or -1.
func (d *openFile) curBand(bands []anchorBand) int {
	if d.from == nil || d.from.ov == nil {
		return -1
	}
	return bandOf(bands, d.from.ov.anchors, d.anchorCur, len(d.p.lines))
}

// bandKind is how a line sits under the bands.
type bandKind int

const (
	bandNone bandKind = iota
	bandOther
	bandCurrent
)

// bandKindAt is line's kind (1-based): the current band wins an overlap.
func bandKindAt(bands []anchorBand, cur, line int) bandKind {
	k := bandNone
	for i, b := range bands {
		if b.start <= line && line <= b.end {
			if i == cur {
				return bandCurrent
			}
			k = bandOther
		}
	}
	return k
}

// gutterMark is a line's mark in the 2-column gutter: the current band,
// another band, a note's lines — in that order — or nothing.
func gutterMark(k bandKind, noted bool) string {
	switch {
	case k == bandCurrent:
		return "┃ "
	case k == bandOther:
		return "╎ "
	case noted:
		return "│ "
	}
	return ""
}

// bandKey gives the focused document's anchor bands n / p. Declined (the
// key keeps its other meanings) when the document has none.
func (m Model) bandKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	s := msg.String()
	if s != "n" && s != "p" {
		return m, nil, false
	}
	d, ok := m.focusedDoc()
	if !ok || d.bands() == nil {
		return m, nil, false
	}
	dir := 1
	if s == "p" {
		dir = -1
	}
	return m.stepAnchorBand(d, dir), nil, true
}

// stepAnchorBand moves d to its next (dir 1) / previous band: the cursor on
// its first line, centred as an anchor open lands, the band current, and the
// overview's selection on its anchor so backspace comes back there.
func (m Model) stepAnchorBand(d *openFile, dir int) Model {
	bs := d.bands()
	k, wrapped := stepBand(bs, d.curBand(bs), d.p.cur+1, dir)
	if k < 0 {
		return m
	}
	b, ov := bs[k], d.from
	a := ov.ov.anchors[b.i]
	d.anchorCur = a.dest
	_, rows, _, _ := m.activePreview()
	d.pendingLine, d.pendingEnd = b.start, 0
	d.landPendingLine(rows)
	vr, _ := m.viewerGeom()
	ov.selectAnchor(b.i, vr)
	label := a.target.Label
	if label == "" {
		label = a.dest
	}
	if wrapped {
		m.statusMsg = i18n.T("anchor %d/%d in this file · %s · wrapped", k+1, len(bs), label)
	} else {
		m.statusMsg = i18n.T("anchor %d/%d in this file · %s", k+1, len(bs), label)
	}
	return m
}

// bandRows are the . menu's n / p rows in a file with bands.
func (m Model) bandRows(d *openFile) []actionRow {
	if d.bands() == nil {
		return nil
	}
	return []actionRow{
		{id: "anchor-next", key: "n", label: i18n.T("Next anchor in this file"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.stepAnchorBand(d, 1), nil
		}},
		{id: "anchor-prev", key: "p", label: i18n.T("Previous anchor in this file"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.stepAnchorBand(d, -1), nil
		}},
	}
}
