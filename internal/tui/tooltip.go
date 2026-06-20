package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// tooltipStyle is the floating strip showing a truncated row's full text.
// Distinct from selectedRow (reverse video): black on yellow reads as an
// annotation layered over the UI.
var tooltipStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11"))

// tooltipMaxLines caps the strip's height; longer content re-truncates with …
const tooltipMaxLines = 3

// tooltip returns the styled lines and overlay position of the full-text
// strip for the focused panel's selected row, when that row is truncated in
// its panel. ok is false when nothing should be shown. Geometry comes from
// the same layout()/panelView/windowRows sources the renderer uses, so the
// two cannot drift.
func (m Model) tooltip() (lines []string, x, y int, ok bool) {
	// While the files view's tree side is focused, the commits selection is
	// not the active row — describing it would be misleading.
	if !m.panelFocused(m.focus) {
		return nil, 0, 0, false
	}
	// The reveal only makes sense in cutoff mode. In wrap the row is already
	// fully visible across wrapped lines; in scroll the user pans to read it.
	if m.dispModes[m.focus] != modeCutoff {
		return nil, 0, 0, false
	}
	g := m.layout()
	p := m.focus
	boxH := g.boxH[p]
	if boxH <= 0 {
		return nil, 0, 0, false
	}
	rows, idx := m.panelView(p)
	sel := m.sel[p]
	if len(rows) == 0 || sel < 0 || sel >= len(rows) {
		return nil, 0, 0, false
	}
	// A panel may supply an UN-elided parallel row (the Commits panel does, for
	// the trimmed branch-name column). When it differs from the displayed row,
	// reveal it even if the row otherwise fits; the displayed row still contains
	// the … so the plain truncation check below would miss it.
	content := rows[sel]
	if fr, ok := m.listFor(p).(fullRower); ok && sel < len(idx) {
		if full := fr.Full(idx[sel]); full != content {
			content = full
		}
	}
	boxW := g.leftW
	if p == panelCommits {
		boxW = g.rightW
	}
	innerW := boxW - 4 // mirrors renderPanel: border (2) + padding (2)
	if content == rows[sel] && !rowTruncated("> "+rows[sel], innerW) {
		return nil, 0, 0, false // shown in full and nothing extra to reveal
	}
	rowsCap := boxH - 3 // mirrors renderPanel: borders + label line
	if rowsCap < 1 {
		return nil, 0, 0, false
	}
	_, selInWin, _ := windowRows(rows, rowsCap, sel)
	origin := g.pos[p]
	rowY := origin.y + 2 + selInWin // top border + label line

	raw := wrapWidth(content, g.w, tooltipMaxLines)
	width := 0
	for _, l := range raw {
		if w := lipgloss.Width(l); w > width {
			width = w
		}
	}
	x = origin.x + 2 // the panel's content edge
	if x+width > g.w {
		x = g.w - width
	}
	if x < 0 {
		x = 0
	}
	y = tooltipY(rowY, len(raw))

	lines = make([]string, len(raw))
	for i, l := range raw {
		lines[i] = tooltipStyle.Render(padRight(l, width))
	}
	return lines, x, y, true
}

// tooltipY places an n-line strip directly above the row at rowY, flipping to
// directly below when there is no room above.
func tooltipY(rowY, n int) int {
	if y := rowY - n; y >= 0 {
		return y
	}
	return rowY + 1
}

// wrapWidth greedily wraps s into display-width-aware lines of at most w
// columns, capped at maxLines. If content remains past the cap, the last
// line is re-truncated to end in …
func wrapWidth(s string, w, maxLines int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	r := []rune(s)
	for len(r) > 0 && len(out) < maxLines {
		n, width := 0, 0
		for n < len(r) {
			cw := lipgloss.Width(string(r[n]))
			if width+cw > w {
				break
			}
			width += cw
			n++
		}
		if n == 0 {
			n = 1 // a single glyph wider than w: emit it rather than loop forever
		}
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	if len(r) > 0 && len(out) > 0 {
		// Remainder exists: force an ellipsis onto the (full-width) last line.
		out[len(out)-1] = truncate(out[len(out)-1]+string(r), w)
	}
	return out
}
