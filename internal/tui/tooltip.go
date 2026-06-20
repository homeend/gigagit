package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// tooltipStyle highlights the inline full-text reveal drawn over a truncated
// row. Distinct from selectedRow (reverse video): black on yellow reads as an
// annotation layered over the UI.
var tooltipStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("11"))

// tooltip returns the styled line and overlay position of the full-text reveal
// for the focused panel's selected row, when that row is truncated in its
// panel. The reveal is drawn inline on the row's own line and overflows the
// panel's right border (up to the screen edge), so it never covers the panel's
// top bar. ok is false when nothing should be shown. Geometry comes from the
// same layout()/panelView/windowRows sources the renderer uses, so the two
// cannot drift.
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

	x = origin.x + 2 // the panel's content edge
	avail := g.w - x // room from the content edge to the screen's right edge
	if avail < 1 {
		return nil, 0, 0, false
	}
	// Draw the full text inline, on the selected row's own line, overflowing the
	// panel's right border (clipped only at the screen edge). Replacing the
	// truncated row in place — rather than floating a strip above it — keeps the
	// reveal off the panel's top bar, which the old strip covered whenever the
	// top row was selected. Single line: it spills past the window instead of
	// wrapping.
	lines = []string{tooltipStyle.Render(truncate(content, avail))}
	y = rowY
	return lines, x, y, true
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
