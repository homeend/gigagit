package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/syntax"
)

// runMask is a per-display-rune paint mask for a winCell body: a syntax class
// per rune plus an emphasis flag per rune. The zero value — both nil — is the
// plain path every uncoloured cell takes. It is deliberately generic rather
// than syntax-specific: phase 6's in-view search paints its hits through the
// same entry point by filling emph (spec §4.3).
type runMask struct {
	cls  []syntax.Class
	emph []bool
}

// empty reports the plain path (no runs to paint).
func (m runMask) empty() bool { return len(m.cls) == 0 && len(m.emph) == 0 }

// slice returns n runes of the mask starting at off, padding with Plain/false
// where a side runs out (a cutoff ellipsis, a clamped token end) and reading
// an out-of-range window as fully plain. A masked cell always yields n entries
// on BOTH sides so styledRuns can index either without a bounds check.
//
// It always ALLOCATES: a cell's mask is a shared cache value (the picker hands
// the same sanLine to the grid and to the output pane), so the layout must
// never write through to it — cellPieces' ellipsis fix relies on that.
func (m runMask) slice(off, n int) runMask {
	if m.empty() || n <= 0 {
		return runMask{}
	}
	if off < 0 {
		off = 0
	}
	out := runMask{cls: make([]syntax.Class, n), emph: make([]bool, n)}
	for i := 0; i < n; i++ {
		j := off + i
		if j < len(m.cls) {
			out.cls[i] = m.cls[j]
		}
		if j < len(m.emph) {
			out.emph[i] = m.emph[j]
		}
	}
	return out
}

// pad returns the mask with n leading plain runes — for a body that carries a
// fixed indent the lexer's runs do not cover (the picker's literal rows).
func (m runMask) pad(n int) runMask {
	if m.empty() || n <= 0 {
		return m
	}
	return runMask{
		cls:  append(make([]syntax.Class, n), m.cls...),
		emph: append(make([]bool, n), m.emph...),
	}
}

// winCell is one cell of a two-column window: a fixed gutter (shown verbatim,
// never transformed — the cursor marker + checkbox live here) and a body the
// display mode transforms. style is applied to the whole padded cell after
// slicing, so width math stays ANSI-safe. The zero value is a blank cell.
//
// mask is an optional paint mask over the body's DISPLAY runes (so
// len(mask.cls) == len([]rune(body))). The layout slices it alongside the body
// in all three modes, so a coloured run lands on the right columns after a
// cutoff, a horizontal scroll or a wrap. An empty mask is the byte-identical
// plain path, and a cell whose style REVERSES video drops the mask — reverse
// swaps foreground and background, so per-token colours would paint per-token
// backgrounds (the same ruling winRow.cls follows).
type winCell struct {
	gutter string
	body   string
	style  lipgloss.Style
	mask   runMask
}

// colRow is one logical row: a full-width row (full != nil, spanning the whole
// width) OR a paired left/right row (left != nil && right != nil).
type colRow struct {
	full        *winCell
	left, right *winCell
}

// twoColOpts configures renderTwoCol. anchor is the colRow index kept visible.
// vshift slides the anchored window by that many display lines (free
// view-scroll); the render clamps it to the content and reports the
// effective shift back so callers can store the clamped value.
type twoColOpts struct {
	w, h    int
	sep     string
	mode    dispMode
	hscroll int
	anchor  int
	vshift  int
}

// cellPiece is one laid-out display segment of a cell: the frozen gutter (or,
// on a wrap continuation, its blank indent), the body slice itself, and that
// slice's paint mask. Keeping the prefix out of the body is what lets the
// masked path paint the body run by run while the gutter and the trailing
// padding stay under the cell's style.
type cellPiece struct {
	pre  string
	body string
	mask runMask
}

// cellPieces lays a cell's body out at width under mode and returns one piece
// per display segment. It is cellSegs split in two; cellSegs is now a wrapper
// over it, so the two can never disagree about the layout.
func cellPieces(c *winCell, width int, mode dispMode, hscroll int) []cellPiece {
	if c == nil {
		return []cellPiece{{}}
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
			return []cellPiece{{pre: c.gutter}}
		}
		masks := wrapSegMasks(c.body, c.mask, ws)
		indent := strings.Repeat(" ", gw)
		out := make([]cellPiece, len(ws))
		for i, s := range ws {
			pre := indent
			if i == 0 {
				pre = c.gutter
			}
			out[i] = cellPiece{pre: pre, body: s, mask: masks[i]}
		}
		return out
	case modeScroll:
		body := hslice(c.body, hscroll, bodyW)
		return []cellPiece{{
			pre:  c.gutter,
			body: body,
			mask: c.mask.slice(hscrollRuneOff(c.body, hscroll), len([]rune(body))),
		}}
	default: // modeCutoff
		body := truncate(c.body, bodyW)
		m := c.mask.slice(0, len([]rune(body)))
		// truncate keeps a prefix and APPENDS "…" (it does not replace a kept
		// rune), so the mask's last slot lands on the class of the first
		// DROPPED rune. Force it plain so the ellipsis never wears a colour it
		// did not earn — window.go does exactly this for winRow.cls.
		if !m.empty() && lipgloss.Width(c.body) > bodyW {
			m.cls[len(m.cls)-1] = syntax.Plain
			m.emph[len(m.emph)-1] = false
		}
		return []cellPiece{{pre: c.gutter, body: body, mask: m}}
	}
}

// wrapSegMasks maps a cell's paint mask onto the segments cellPieces produced
// in wrap mode. wrapWidth (at the huge line cap cellPieces passes) slices runes
// verbatim and never rewrites them, so each segment's mask is the slice of the
// cell's mask at the running rune offset. The layout is VERIFIED against the
// body before it is trusted: if the segments do not reconstruct it, every
// segment is reported empty and the cell renders plain rather than mis-coloured.
func wrapSegMasks(body string, m runMask, segs []string) []runMask {
	out := make([]runMask, len(segs))
	if m.empty() {
		return out
	}
	var joined strings.Builder
	off := 0
	for i, s := range segs {
		n := len([]rune(s))
		out[i] = m.slice(off, n)
		joined.WriteString(s)
		off += n
	}
	if joined.String() != body {
		return make([]runMask, len(segs))
	}
	return out
}

// cellSegs lays a cell's body out at width under mode, returning the raw
// (unstyled, unpadded) display segments with the gutter on the first segment
// and a blank indent of the gutter's width on wrap continuations.
func cellSegs(c *winCell, width int, mode dispMode, hscroll int) []string {
	ps := cellPieces(c, width, mode, hscroll)
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.pre + p.body
	}
	return out
}

// pieceOrBlank returns the kth piece or a blank one when the cell ran out (the
// blank-pad that keeps a wrapped pair registered).
func pieceOrBlank(ps []cellPiece, k int) cellPiece {
	if k < len(ps) {
		return ps[k]
	}
	return cellPiece{}
}

// renderPiece renders one laid-out segment padded to w: pre+body under style —
// byte-identical to styleCell(style, pre+body, w) — or, for a masked piece,
// the body painted run by run (styledRuns) with the prefix and the trailing
// padding under style. Mirrors renderWindow's colouredLine.
func renderPiece(style lipgloss.Style, p cellPiece, w int) string {
	if p.mask.empty() || style.GetReverse() {
		return styleCell(style, p.pre+p.body, w)
	}
	disp := []rune(p.body)
	m := p.mask.slice(0, len(disp)) // defensive: exactly one entry per rune
	var b strings.Builder
	if p.pre != "" {
		b.WriteString(style.Render(p.pre))
	}
	b.WriteString(styledRuns(disp, m.emph, m.cls, style))
	if pad := w - lipgloss.Width(p.pre) - lipgloss.Width(p.body); pad > 0 {
		b.WriteString(style.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
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
func renderTwoCol(rows []colRow, o twoColOpts) ([]string, int) {
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

	// Fast path: outside wrap mode every row is exactly ONE display line
	// (scroll pans, cutoff truncates — neither expands), so the window can
	// be computed in row space and only the h visible rows built and styled.
	// The full-document pass below styles every line before windowing —
	// O(doc) of lipgloss work per frame, measured 35ms on a 5000-row doc.
	if o.mode != modeWrap {
		start := windowStart(len(rows), h, o.anchor)
		eff := 0
		if o.vshift != 0 {
			maxStart := len(rows) - h
			if maxStart < 0 {
				maxStart = 0
			}
			s := start + o.vshift
			if s > maxStart {
				s = maxStart
			}
			if s < 0 {
				s = 0
			}
			eff = s - start
			start = s
		}
		out := make([]string, 0, h)
		for i := 0; i < h; i++ {
			idx := start + i
			if idx >= len(rows) {
				out = append(out, padRight("", w))
				continue
			}
			r := rows[idx]
			if r.full != nil {
				out = append(out, renderPiece(r.full.style, cellPieces(r.full, w, o.mode, o.hscroll)[0], w))
				continue
			}
			left := renderPiece(cellStyle(r.left), cellPieces(r.left, colW, o.mode, o.hscroll)[0], colW)
			right := renderPiece(cellStyle(r.right), cellPieces(r.right, colW, o.mode, o.hscroll)[0], colW)
			out = append(out, left+o.sep+right)
		}
		return out, eff
	}

	type dline struct {
		text string
		row  int
	}
	var dl []dline
	for ri, r := range rows {
		if r.full != nil {
			for _, p := range cellPieces(r.full, w, o.mode, o.hscroll) {
				dl = append(dl, dline{text: renderPiece(r.full.style, p, w), row: ri})
			}
			continue
		}
		ls := cellPieces(r.left, colW, o.mode, o.hscroll)
		rs := cellPieces(r.right, colW, o.mode, o.hscroll)
		n := len(ls)
		if len(rs) > n {
			n = len(rs)
		}
		for k := 0; k < n; k++ {
			left := renderPiece(cellStyle(r.left), pieceOrBlank(ls, k), colW)
			right := renderPiece(cellStyle(r.right), pieceOrBlank(rs, k), colW)
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
	eff := 0
	if o.vshift != 0 {
		maxStart := len(dl) - h
		if maxStart < 0 {
			maxStart = 0
		}
		s := start + o.vshift
		if s > maxStart {
			s = maxStart
		}
		if s < 0 {
			s = 0
		}
		eff = s - start
		start = s
	}

	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		idx := start + i
		if idx < len(dl) {
			out = append(out, dl[idx].text)
		} else {
			out = append(out, padRight("", w))
		}
	}
	return out, eff
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
