package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// winCell is one cell of a two-column window: a fixed gutter (shown verbatim,
// never transformed — the cursor marker + checkbox live here) and a body the
// display mode transforms. style is applied to the whole padded cell after
// slicing, so width math stays ANSI-safe. The zero value is a blank cell.
type winCell struct {
	gutter string
	body   string
	style  lipgloss.Style
}

// colRow is one logical row: a full-width row (full != nil, spanning the whole
// width) OR a paired left/right row (left != nil && right != nil).
type colRow struct {
	full        *winCell
	left, right *winCell
}

// twoColOpts configures renderTwoCol. anchor is the colRow index kept visible.
type twoColOpts struct {
	w, h    int
	sep     string
	mode    dispMode
	hscroll int
	anchor  int
}

// cellSegs lays a cell's body out at width under mode, returning the raw
// (unstyled, unpadded) display segments with the gutter on the first segment
// and a blank indent of the gutter's width on wrap continuations.
func cellSegs(c *winCell, width int, mode dispMode, hscroll int) []string {
	if c == nil {
		return []string{""}
	}
	gw := lipgloss.Width(c.gutter)
	bodyW := width - gw
	if bodyW < 1 {
		bodyW = 1
	}
	switch mode {
	case modeWrap:
		ws := wrapWidth(c.body, bodyW, 1<<20)
		if len(ws) == 0 {
			return []string{c.gutter}
		}
		indent := strings.Repeat(" ", gw)
		out := make([]string, len(ws))
		for i, s := range ws {
			if i == 0 {
				out[i] = c.gutter + s
			} else {
				out[i] = indent + s
			}
		}
		return out
	case modeScroll:
		return []string{c.gutter + hslice(c.body, hscroll, bodyW)}
	default: // modeCutoff
		return []string{c.gutter + truncate(c.body, bodyW)}
	}
}

// segOrBlank returns the kth segment or "" when the cell ran out (the blank-pad that
// keeps a wrapped pair registered).
func segOrBlank(segs []string, k int) string {
	if k < len(segs) {
		return segs[k]
	}
	return ""
}

// renderTwoCol lays rows out under o and returns exactly o.h display lines, each
// padded to o.w columns. The horizontal mode applies per cell body; wrapped
// left/right pairs are aligned by padding the shorter side; the vertical window
// is shared and anchored to o.anchor.
func renderTwoCol(rows []colRow, o twoColOpts) []string {
	w, h := o.w, o.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	colW := (w - lipgloss.Width(o.sep)) / 2
	if colW < 1 {
		colW = 1
	}

	type dline struct {
		text string
		row  int
	}
	var dl []dline
	for ri, r := range rows {
		if r.full != nil {
			for _, s := range cellSegs(r.full, w, o.mode, o.hscroll) {
				dl = append(dl, dline{text: styleCell(r.full.style, s, w), row: ri})
			}
			continue
		}
		ls := cellSegs(r.left, colW, o.mode, o.hscroll)
		rs := cellSegs(r.right, colW, o.mode, o.hscroll)
		n := len(ls)
		if len(rs) > n {
			n = len(rs)
		}
		for k := 0; k < n; k++ {
			left := styleCell(cellStyle(r.left), segOrBlank(ls, k), colW)
			right := styleCell(cellStyle(r.right), segOrBlank(rs, k), colW)
			dl = append(dl, dline{text: left + o.sep + right, row: ri})
		}
	}

	anchorLine := 0
	for i, d := range dl {
		if d.row == o.anchor {
			anchorLine = i
			break
		}
	}
	start := windowStart(len(dl), h, anchorLine)

	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		idx := start + i
		if idx < len(dl) {
			out = append(out, dl[idx].text)
		} else {
			out = append(out, padRight("", w))
		}
	}
	return out
}

// cellStyle returns a cell's style (zero value for a nil/blank cell).
func cellStyle(c *winCell) lipgloss.Style {
	if c == nil {
		return lipgloss.Style{}
	}
	return c.style
}

// styleCell pads raw to w columns then applies style (style after padding keeps
// the width-based slicing ANSI-safe).
func styleCell(style lipgloss.Style, raw string, w int) string {
	return style.Render(padRight(raw, w))
}
