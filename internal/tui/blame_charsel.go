package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// The blame view as a character-selection host (charsel.go): one row per
// blamed line, its SOURCE text (a tab is one rune, copied as a tab); the
// painter maps a column to the sanitized display through dispCol. The
// stripe paints the code only — the commit gutter is the row's prefix.

// blameText is a blamed line's source text as the view shows it: a CRLF
// line's trailing \r is line-ending layout, so no copy carries it.
func blameText(ln model.BlameLine) string { return strings.TrimSuffix(ln.Content, "\r") }

func (b *blameView) charRows() []charRow { return b.csRows }
func (b *blameView) charPage() int       { return max(b.csPage, 1) }

// charSpans is the selection on line i as spans in its display rune space;
// nil when the mode paints nothing there.
func (b *blameView) charSpans(i int) []hitSpan {
	if !b.cs.on || i < 0 || i >= len(b.csRows) || !b.cs.touches(i) {
		return nil
	}
	return charSpansDisp(b.cs, i, string(b.csRows[i].text))
}

// charEmph lays the selection over line i's emphasis mask (the search hits)
// for its display text: the selection paints over a hit.
func (b *blameView) charEmph(emph []emphLevel, i int, disp string) []emphLevel {
	if sp := b.charSpans(i); sp != nil {
		return overlayHits(emph, 0, len([]rune(disp)), sp)
	}
	return emph
}

// charEmphOn is line i's selection mask alone over its display text.
func (b *blameView) charEmphOn(i int, disp string) []emphLevel { return b.charEmph(nil, i, disp) }

// charRoom gives an empty line one blank cell while the mode paints on it.
func (b *blameView) charRoom(i int, disp string) string {
	if !b.cs.on {
		return disp
	}
	return charRoom(b.cs, i, disp)
}

// codeWidth is the code column's width: the screen less the gutter and its
// │ (render's arithmetic).
func (b *blameView) codeWidth(m Model) int {
	w, _ := m.overlayDims()
	gw := min(blameGutterW, w-10)
	return max(w-max(gw, 0)-1, 1)
}

// charPan keeps the cursor inside the code column in scroll mode.
func (b *blameView) charPan(m Model) {
	if b.mode != modeScroll || !b.cs.on || b.cs.cur.row >= len(b.csRows) {
		return
	}
	b.hscroll = charPanCells(string(b.csRows[b.cs.cur.row].text), b.cs.cur.col, b.hscroll, b.codeWidth(m))
}

// charEnded puts back the display mode v swapped away (cutoff runs as
// scroll while the mode is on) once the mode is off.
func (b *blameView) charEnded() {
	if b.cs.on || !b.csSwapped {
		return
	}
	b.mode, b.csSwapped, b.hscroll = b.csMode, false, 0
}

// blameCharKey gives the character selection every key while it is on, and
// v when it is off; it runs BEFORE the line selection and the search hooks.
// handled == true means the caller must return immediately.
func (m Model) blameCharKey(b *blameView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if b == nil {
		return m, nil, false
	}
	b.csPage = m.blameBodyRows()
	if !b.cs.on {
		if msg.String() != "v" || b.search.typing {
			return m, nil, false
		}
		if b.loading || b.err != nil || len(b.lines) == 0 {
			m.statusMsg = i18n.T("▸ nothing to select")
			return m, nil, true
		}
		b.lsel.clear() // one selection kind at a time
		b.csRows = make([]charRow, len(b.lines))
		for i, ln := range b.lines {
			b.csRows[i] = charRow{text: []rune(blameText(ln))}
		}
		if !b.cs.enter(b.csRows, pos{row: b.sel}) {
			m.statusMsg = i18n.T("▸ nothing to select")
			return m, nil, true
		}
		if b.mode == modeCutoff { // a cut line hides the cursor: scroll while on
			b.csMode, b.csSwapped, b.mode, b.hscroll = b.mode, true, modeScroll, 0
		}
		b.sel = b.cs.cur.row
		return m, nil, true
	}
	res := charSelKey(&b.cs, b, msg)
	if !res.handled {
		return m, nil, false
	}
	if b.cs.on {
		b.sel = b.cs.cur.row // the line cursor (and the window) follows
		b.charPan(m)
	} else {
		b.charEnded()
	}
	if res.notice != "" {
		m.statusMsg = res.notice
	}
	if res.copy != "" {
		return m, m.copyToClipboardCmd(copiedCharsText(res.count), res.copy), true
	}
	return m, nil, true
}
