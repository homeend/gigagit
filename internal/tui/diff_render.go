package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/textdiff"
)

var (
	diffDelCell = lipgloss.NewStyle().Background(lipgloss.Color("52"))  // dark red
	diffAddCell = lipgloss.NewStyle().Background(lipgloss.Color("22"))  // dark green
	diffGapCell = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim · filler
	diffGutter  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	diffFold    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))            // dim fold rule
	diffEmph    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")) // bright fg over the hot cell bg
)

const diffHint = "[↑↓] scroll  [pgup/pgdn] page  [n/p] next/prev change  [f] toggle partial  [w] wrap  [esc] close  [q] quit"

// cellSeg is one pane's text for one display row: the sanitized display runes
// and the parallel emphasis mask, already ≤ the pane's text width. A zero
// cellSeg renders blank.
type cellSeg struct {
	disp []rune
	emph []bool
}

// wrapCells splits a sanitized (disp, emph) line into segments each ≤ tw
// display columns. It greedily fills to tw, then breaks after the last space
// in the segment (word-wrap); a word longer than tw is hard-broken at the fill
// boundary, and a single rune wider than tw is taken alone (never an empty
// loop). The emph mask is sliced alongside so emphasis survives a split. An
// empty input yields one empty segment (so the row still draws a line).
func wrapCells(disp []rune, emph []bool, tw int) []cellSeg {
	if tw < 1 {
		tw = 1
	}
	if len(disp) == 0 {
		return []cellSeg{{}}
	}
	var segs []cellSeg
	start := 0
	for start < len(disp) {
		end, width := start, 0
		for end < len(disp) {
			rw := lipgloss.Width(string(disp[end]))
			if width+rw > tw {
				break
			}
			width += rw
			end++
		}
		if end == start { // a single rune wider than tw
			end = start + 1
		}
		brk := end
		if end < len(disp) { // more to come: prefer a word boundary
			sp := -1
			for j := start; j < end; j++ {
				if disp[j] == ' ' {
					sp = j
				}
			}
			if sp > start {
				brk = sp + 1 // keep the space on this segment
			}
		}
		segs = append(segs, cellSeg{disp: disp[start:brk], emph: emph[start:brk]})
		start = brk
	}
	return segs
}

// sanitizeLine makes raw file content safe to render on one line: tabs
// expand to a fixed 4-column stop (lipgloss.Width doesn't expand them but
// the terminal would, pushing text through the pane separator), a trailing
// \r is stripped, remaining control characters become '·'. Display only —
// Compare sees the raw lines.
func sanitizeLine(s string) string {
	s = strings.TrimSuffix(s, "\r")
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch {
		case r == '\t':
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case r < 0x20 || r == 0x7f:
			b.WriteRune('·')
			col++
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// renderDiffView draws the whole screen: header, aligned panes, hint line.
func (m Model) renderDiffView() string {
	v := m.diffView
	w, h := m.overlayDims()
	body := h - 2 // header + hint are the only chrome (must match diffBodyRows)
	if body < 1 {
		body = 1
	}

	note := ""
	switch {
	case v.truncated:
		note = "  (alignment skipped: large file)"
	// loading/err/binary/tooLarge render their own body state below; the
	// guards here keep the note from doubling up with them.
	case !v.loading && v.err == nil && !v.binary && !v.tooLarge && len(v.blocks) == 0:
		note = "  (no content difference)"
	}
	head := "diff: " + v.title + "  " + v.context + note
	rangeStr := ""
	if n := len(v.disp); n > 0 {
		hi := v.offset + body
		if hi > n {
			hi = n
		}
		rangeStr = fmt.Sprintf("rows %d–%d/%d", v.offset+1, hi, n)
	}
	avail := w - lipgloss.Width(rangeStr) - 2
	if avail < 1 {
		avail = 1
	}
	head = padRight(truncate(head, avail), avail)
	header := truncate(head+"  "+rangeStr, w)

	lines := make([]string, 0, h)
	lines = append(lines, header)
	switch {
	case v.loading:
		lines = append(lines, "  (loading…)")
	case v.err != nil:
		lines = append(lines, truncate("  error: "+v.err.Error(), w))
	case v.binary:
		lines = append(lines, "  (binary file)")
	case v.tooLarge:
		lines = append(lines, "  (file too large)")
	default:
		lines = append(lines, m.diffPaneLines(v, w, body)...)
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	lines = append(lines, truncate(diffHint, w))
	return strings.Join(lines, "\n")
}

// gutterWidth is the line-number column width, derived from the full rows so it
// is stable across mode/wrap toggles. Minimum 3.
func gutterWidth(full []textdiff.Row) int {
	maxNo := 0
	for _, r := range full {
		if r.LeftNo > maxNo {
			maxNo = r.LeftNo
		}
		if r.RightNo > maxNo {
			maxNo = r.RightNo
		}
	}
	g := len(fmt.Sprint(maxNo))
	if g < 3 {
		g = 3
	}
	return g
}

// diffPaneLines renders the visible window of display rows. A fold dRow is a
// full-width separator. Otherwise: wrap off draws the row via diffCell (raw
// text, truncated — byte-identical to before); wrap on draws each side's
// pre-wrapped segment via segCell.
func (m Model) diffPaneLines(v *diffView, w, body int) []string {
	paneW := (w - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	gut := gutterWidth(v.full)

	out := make([]string, 0, body)
	for i := v.offset; i < v.offset+body && i < len(v.disp); i++ {
		dr := v.disp[i]
		if dr.fold > 0 {
			out = append(out, foldSeparator(dr.fold, w))
			continue
		}
		r := dr.row
		if !v.wrap {
			left := diffCell(r.LeftNo, r.Left, gut, paneW,
				r.Kind == textdiff.Add,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell, r.LeftSpans)
			right := diffCell(r.RightNo, r.Right, gut, paneW,
				r.Kind == textdiff.Del,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell, r.RightSpans)
			out = append(out, left+"│"+right)
			continue
		}
		leftGap := r.Kind == textdiff.Add
		rightGap := r.Kind == textdiff.Del
		leftNo, rightNo := 0, 0
		if dr.first && !leftGap {
			leftNo = r.LeftNo
		}
		if dr.first && !rightGap {
			rightNo = r.RightNo
		}
		left := segCell(leftNo, dr.left, gut, paneW, leftGap,
			r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell)
		right := segCell(rightNo, dr.right, gut, paneW, rightGap,
			r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell)
		out = append(out, left+"│"+right)
	}
	return out
}

// segCell renders one pane's pre-wrapped segment into a width-col cell: gutter
// (number when no>0, blank on a continuation) + the styled, padded body. gap
// draws the · filler (absent side). hot applies the add/del background;
// emphasis rides in seg.emph.
func segCell(no int, seg cellSeg, gut, width int, gap, hot bool, hotStyle lipgloss.Style) string {
	if gap {
		return diffGapCell.Render(strings.Repeat("·", width))
	}
	if gut > width-2 { // degenerate pane: keep the cell inside its width
		gut = width - 2
		if gut < 1 {
			gut = 1
		}
	}
	num := strings.Repeat(" ", gut+1)
	if no > 0 {
		num = fmt.Sprintf("%*d ", gut, no)
	}
	tw := width - gut - 1
	if tw < 1 {
		tw = 1
	}
	base := lipgloss.NewStyle()
	if hot {
		base = hotStyle
	}
	body := styledRuns(seg.disp, seg.emph, base)
	if pad := tw - lipgloss.Width(string(seg.disp)); pad > 0 {
		body += base.Render(strings.Repeat(" ", pad))
	}
	return diffGutter.Render(truncate(num, gut+1)) + body
}

// foldSeparator renders a fold marker as a centered label on a dim rule
// spanning the full width.
func foldSeparator(n, w int) string {
	label := fmt.Sprintf(" ⤬ %d unchanged lines ", n)
	if n == 1 {
		label = " ⤬ 1 unchanged line "
	}
	lw := lipgloss.Width(label)
	if lw >= w {
		return diffFold.Render(truncate(label, w))
	}
	left := (w - lw) / 2
	right := w - lw - left
	return diffFold.Render(strings.Repeat("─", left) + label + strings.Repeat("─", right))
}

// diffCell renders one pane cell: gutter + text, or the dim gap filler. With
// no spans (plain mode, non-Changed rows, or enrichment give-up) it is
// byte-identical to the pre-enrichment renderer; with spans it layers
// intraline emphasis over the hot cell background.
func diffCell(no int, text string, gut, width int, gap, hot bool, hotStyle lipgloss.Style, spans []textdiff.Span) string {
	if gap {
		return diffGapCell.Render(strings.Repeat("·", width))
	}
	if gut > width-2 { // degenerate pane: keep the cell inside its width
		gut = width - 2
		if gut < 1 {
			gut = 1
		}
	}
	num := fmt.Sprintf("%*d ", gut, no)
	tw := width - gut - 1
	if tw < 1 {
		tw = 1
	}
	var bodyTxt string
	if hot && len(spans) > 0 {
		bodyTxt = hotEmphBody(text, spans, tw, hotStyle)
	} else {
		bodyTxt = padRight(truncate(sanitizeLine(text), tw), tw)
		if hot {
			bodyTxt = hotStyle.Render(bodyTxt)
		}
	}
	return diffGutter.Render(truncate(num, gut+1)) + bodyTxt
}

// hotEmphBody renders a Changed cell's text into a tw-column body: sanitized
// like sanitizeLine, the whole cell carrying hotStyle, with the runes whose
// raw index falls in a span additionally wearing diffEmph. Truncation mirrors
// truncate()'s trailing ellipsis.
func hotEmphBody(text string, spans []textdiff.Span, tw int, hotStyle lipgloss.Style) string {
	disp, emph := sanitizeSpans(text, spans)
	if lipgloss.Width(string(disp)) <= tw {
		body := styledRuns(disp, emph, hotStyle)
		if pad := tw - lipgloss.Width(string(disp)); pad > 0 {
			body += hotStyle.Render(strings.Repeat(" ", pad))
		}
		return body
	}
	if tw == 1 {
		return hotStyle.Render("…")
	}
	w, cut := 0, len(disp)
	for i, r := range disp {
		rw := lipgloss.Width(string(r))
		if w+rw+1 > tw { // reserve one column for the ellipsis
			cut = i
			break
		}
		w += rw
	}
	body := styledRuns(disp[:cut], emph[:cut], hotStyle) + hotStyle.Render("…")
	// A double-width rune at the cut boundary can leave the body one column
	// short; pad to tw so the row width matches the plain path exactly.
	if pad := tw - lipgloss.Width(body); pad > 0 {
		body += hotStyle.Render(strings.Repeat(" ", pad))
	}
	return body
}

// sanitizeSpans expands text exactly as sanitizeLine and returns the display
// runes with a parallel mask marking those whose source raw rune is covered by
// a span. Raw indices are counted over the \r-trimmed text (matching
// sanitizeLine); span ends are clamped to that length.
func sanitizeSpans(s string, spans []textdiff.Span) (disp []rune, emph []bool) {
	s = strings.TrimSuffix(s, "\r")
	runes := []rune(s)
	cover := coverMask(len(runes), spans)
	col := 0
	for raw, r := range runes {
		on := cover[raw]
		switch {
		case r == '\t':
			n := 4 - col%4
			for k := 0; k < n; k++ {
				disp = append(disp, ' ')
				emph = append(emph, on)
			}
			col += n
		case r < 0x20 || r == 0x7f:
			disp = append(disp, '·')
			emph = append(emph, on)
			col++
		default:
			disp = append(disp, r)
			emph = append(emph, on)
			col++
		}
	}
	return disp, emph
}

// coverMask marks raw rune indices [0,n) covered by any span (ends clamped).
func coverMask(n int, spans []textdiff.Span) []bool {
	mask := make([]bool, n)
	for _, sp := range spans {
		lo, hi := sp.Start, sp.End
		if lo < 0 {
			lo = 0
		}
		if hi > n {
			hi = n
		}
		for i := lo; i < hi; i++ {
			mask[i] = true
		}
	}
	return mask
}

// styledRuns renders disp grouping consecutive runes by emph flag: emphasized
// runs wear diffEmph inherited over base (so the cell background shows through),
// the rest just base.
func styledRuns(disp []rune, emph []bool, base lipgloss.Style) string {
	var b strings.Builder
	for i := 0; i < len(disp); {
		j := i + 1
		for j < len(disp) && emph[j] == emph[i] {
			j++
		}
		seg := string(disp[i:j])
		if emph[i] {
			b.WriteString(base.Inherit(diffEmph).Render(seg))
		} else {
			b.WriteString(base.Render(seg))
		}
		i = j
	}
	return b.String()
}
