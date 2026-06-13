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
	diffFold    = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim fold rule
)

const diffHint = "[↑↓] scroll  [pgup/pgdn] page  [n/p] next/prev change  [f] toggle partial  [h] history  [esc] close  [q] quit"

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
	if n := len(v.lines); n > 0 {
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

// diffPaneLines renders the visible window of display lines: a row Line as
// left│right, a fold Line as a full-width dim separator.
func (m Model) diffPaneLines(v *diffView, w, body int) []string {
	paneW := (w - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	maxNo := 0
	for _, r := range v.full { // gutter width from the full rows: stable across toggle
		if r.LeftNo > maxNo {
			maxNo = r.LeftNo
		}
		if r.RightNo > maxNo {
			maxNo = r.RightNo
		}
	}
	gut := len(fmt.Sprint(maxNo))
	if gut < 3 {
		gut = 3
	}

	out := make([]string, 0, body)
	for i := v.offset; i < v.offset+body && i < len(v.lines); i++ {
		ln := v.lines[i]
		if ln.Fold > 0 {
			out = append(out, foldSeparator(ln.Fold, w))
			continue
		}
		r := ln.Row
		left := diffCell(r.LeftNo, r.Left, gut, paneW,
			r.Kind == textdiff.Add, // gap on the left when the line exists only on the right
			r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell)
		right := diffCell(r.RightNo, r.Right, gut, paneW,
			r.Kind == textdiff.Del,
			r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell)
		out = append(out, left+"│"+right)
	}
	return out
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

// diffCell renders one pane cell: gutter + text, or the dim gap filler.
func diffCell(no int, text string, gut, width int, gap, hot bool, hotStyle lipgloss.Style) string {
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
	bodyTxt := padRight(truncate(sanitizeLine(text), tw), tw)
	if hot {
		bodyTxt = hotStyle.Render(bodyTxt)
	}
	return diffGutter.Render(truncate(num, gut+1)) + bodyTxt
}
