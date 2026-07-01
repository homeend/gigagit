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
	// The commit files tree is a separate left-column slot (not a panel). When
	// its side is focused it owns the selection, so reveal its truncated row.
	if m.filesView != nil && m.filesTreeFocused {
		return m.filesTreeReveal()
	}
	// The file preview owns the right column (it replaced the Commits panel) and
	// is a content pager with no truncated-row reveal of its own. Without this the
	// panel branch below would surface the hidden commit row's reveal — a long
	// commit subject that shifts left and lands over the file tree. (Reached only
	// when the preview, not the tree, is focused; the tree-focused path returned
	// above.)
	if m.filesPreview != nil {
		return nil, 0, 0, false
	}
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
	idx := m.displayIndices(p)
	sel := m.sel[p]
	if len(idx) == 0 || sel < 0 || sel >= len(idx) {
		return nil, 0, 0, false
	}
	// Only the selected row is materialized (the Commits panel styles rows
	// lazily; fetching all of them here would re-introduce the per-frame cost).
	l := m.listFor(p)
	rowSel := l.Row(idx[sel])
	// A panel may supply an UN-elided parallel row (the Commits panel does, for
	// the trimmed branch-name column). When it differs from the displayed row,
	// reveal it even if the row otherwise fits; the displayed row still contains
	// the … so the plain truncation check below would miss it.
	content := rowSel
	if fr, ok := l.(fullRower); ok {
		if full := fr.Full(idx[sel]); full != content {
			content = full
		}
	}
	boxW := g.leftW
	if p == panelCommits {
		boxW = g.rightW
	}
	innerW := boxW - 4 // mirrors renderPanel: border (2) + padding (2)
	if content == rowSel && !rowTruncated("> "+rowSel, innerW) {
		return nil, 0, 0, false // shown in full and nothing extra to reveal
	}
	rowsCap := boxH - 3 // mirrors renderPanel: borders + label line
	if rowsCap < 1 {
		return nil, 0, 0, false
	}
	selInWin := sel - windowStart(len(idx), rowsCap, sel)
	origin := g.pos[p]
	rowY := origin.y + 2 + selInWin // top border + label line

	if g.w < 1 {
		return nil, 0, 0, false
	}
	// The decision above (using Full, which carries the graph + padded identity)
	// settles WHEN to reveal. A panel may offer a compact text-only reveal for
	// WHAT to draw — the Commits panel does, so its graph lanes and identity
	// padding don't end up in the reveal strip.
	reveal := content
	if tr, ok := l.(textRevealer); ok {
		if t := tr.TextReveal(idx[sel]); t != "" {
			reveal = t
		}
	}
	line, x := revealLine(reveal, origin.x+2, innerW, g.w)
	return []string{line}, x, rowY, true
}

// revealClipMargin trims a reveal that is wider than the whole terminal this many
// columns short of the right edge, so the trailing … sits clear of the border
// rather than hugging it (the requested behaviour — a subject longer than the
// whole viewport is trimmed to terminal width − 5 and marked with …).
const revealClipMargin = 5

// revealLine builds the styled inline reveal for a row whose full text is
// `content`, displayed at column `contentEdge` in a window of inner width
// `innerW` on a `screenW`-wide screen, and returns the line plus its start
// column. Shared by the panel reveal and the files-tree reveal so both place
// identically: draw the full text inline (never a floating strip, which covered
// the panel's top bar when the top row was selected); fill at least the window's
// inner width so the selected-row reverse-video highlight beneath never peeks out
// to its right; overflow the window's right border, and when the row would run
// off the SCREEN's right edge (a right-hand window has little room to its right)
// shift left so it spills over whatever sits to its left — the reveal may use the
// WHOLE terminal width, not just its own window. Only a row wider than the whole
// terminal is clipped: it is trimmed to screenW − revealClipMargin and marked with
// … so the ellipsis clears the right edge, and the highlight is sized to that
// clipped text (not padded to the full screen), so the yellow strip hugs the text
// instead of trailing blank yellow to the border. Single line, never wrapped.
func revealLine(content string, contentEdge, innerW, screenW int) (line string, x int) {
	x = contentEdge
	full := content
	revealW := lipgloss.Width(full)
	if revealW < innerW {
		revealW = innerW
	}
	if x+revealW > screenW {
		x = screenW - revealW // shift left so the reveal's right edge sits at the screen edge
	}
	if x < 0 {
		// Wider than the whole terminal: pin to the left edge and trim the text to
		// screenW − margin with … so the ellipsis clears the right border. Size the
		// highlight to the clipped text itself (no full-width fill) so the yellow
		// strip hugs the text instead of spanning the whole screen with trailing
		// blank padding.
		x = 0
		textCap := screenW - revealClipMargin
		if textCap < 1 {
			textCap = screenW
		}
		full = truncate(full, textCap)
		revealW = lipgloss.Width(full)
	}
	return tooltipStyle.Render(padRight(full, revealW)), x
}

// filesTreeReveal builds the inline reveal for the commit files tree's selected
// row when it is truncated in the left column. The tree is a separate slot, so
// its geometry is computed here (mirroring renderFilesView) rather than from the
// panel layout. Returns ok=false when nothing should be revealed.
func (m Model) filesTreeReveal() (lines []string, x, y int, ok bool) {
	p := m.filesView
	if p == nil {
		return nil, 0, 0, false
	}
	// Reveal only in cutoff mode (wrap shows the row in full; scroll pans to it).
	if p.mode != modeCutoff {
		return nil, 0, 0, false
	}
	vis := p.visible()
	if len(vis) == 0 || p.sel < 0 || p.sel >= len(vis) {
		return nil, 0, 0, false
	}
	g := m.layout()
	if g.w < 1 || g.leftW < 5 {
		return nil, 0, 0, false
	}
	innerW := g.leftW - 4 // mirrors renderFilesView: border (2) + padding (2)
	content := vis[p.sel].text
	if !rowTruncated("> "+content, innerW) {
		return nil, 0, 0, false // the row is shown in full
	}
	// Row capacity mirrors renderFilesView: box height − borders (2) − title −
	// hint, minus one more when the /-search line is showing.
	rowsCap := g.bodyH - 4
	if p.searchLine() != "" {
		rowsCap--
	}
	if rowsCap < 1 {
		rowsCap = 1
	}
	selInWin := p.sel - windowStart(len(vis), rowsCap, p.sel)
	// Screen row: the left column starts at row 1 (header is row 0); inside it the
	// border + title (+ search line) precede the first data row.
	rowY := 1 + 2 + selInWin // box top (1) + border + title
	if p.searchLine() != "" {
		rowY++
	}
	line, x := revealLine(content, 2, innerW, g.w) // left column content edge = 2
	return []string{line}, x, rowY, true
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
