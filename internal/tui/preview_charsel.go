package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The View-file preview as a character-selection host (charsel.go) — the
// files view's right column and the full-screen viewer alike (activePreview).
// One row per line: a file line's SOURCE text (a tab is one rune, copied as
// a tab; a control rune the preview never shows is not in the row), dead on
// a placeholder or an image row. An overview document is laid out by gg
// itself: its rows carry the layout's wrap joins and hang indents
// (contentLine.cont/hard/lead); its cut view switches to scroll as a file's
// does, re-laid at once (layOut) before the rows are taken. Note boxes under lines are not lines: never rows,
// never copied. The rows reuse the content popup's csRows (the Summary
// popup's display-row layout is its own adapter, content_popup.go).

// previewCharText is a line's selectable text: the raw source minus the
// runes the display drops, minus the layout lead.
func previewCharText(l contentLine) []rune {
	var out []rune
	for _, r := range l.raw {
		if r == '\t' || !dropForDisplay(r) {
			out = append(out, r)
		}
	}
	return out[min(max(l.lead, 0), len(out)):]
}

// previewDispCol is the display rune index of column col of a row's text
// (sanitizeForDisplay: a tab is four cells), behind the line's lead. A column
// past the end is one cell per rune it is past (the empty row's cursor).
func previewDispCol(text []rune, lead, col int) int {
	idx := lead
	for i, r := range text {
		if i >= col {
			return idx
		}
		if r == '\t' {
			idx += 4
		} else {
			idx++
		}
	}
	return idx + max(col-len(text), 0)
}

// previewCharEmph lays the selection over line row's emphasis mask (the
// search hits) for its display text: the selection paints over a hit.
func (p *contentPopup) previewCharEmph(emph []emphLevel, row int, disp string) []emphLevel {
	if !p.cs.on || row < 0 || row >= len(p.csRows) || !p.cs.touches(row) {
		return emph
	}
	text, lead := p.csRows[row].text, 0
	if row < len(p.lines) {
		lead = min(max(p.lines[row].lead, 0), len([]rune(p.lines[row].raw)))
	}
	spans := charSelSpans(p.cs, row)
	for i := range spans {
		spans[i].start = previewDispCol(text, lead, spans[i].start)
		if spans[i].end < 1<<29 {
			spans[i].end = previewDispCol(text, lead, spans[i].end)
		}
	}
	return overlayHits(emph, 0, len([]rune(disp)), spans)
}

// previewCharStale reports whether p.lines is no longer the slice the mode
// was built over (a load, an overview's re-layout).
func (p *contentPopup) previewCharStale() bool {
	if len(p.lines) != len(p.csLines) {
		return true
	}
	return len(p.lines) > 0 && &p.lines[0] != &p.csLines[0]
}

// previewCharLeave ends the mode and puts back the view v switched away from
// (cutoff runs as scroll while the mode is on).
func (p *contentPopup) previewCharLeave() {
	p.cs.leave()
	p.csLines = nil
	if p.csSwapped {
		p.mode, p.csSwapped, p.hscroll = p.csMode, false, 0
	}
}

// previewCharKey gives the focused preview's character selection every key
// while it is on, and v when it is off; it runs BEFORE the line selection and
// the search hooks. handled == true means the caller must return.
func (m Model) previewCharKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p, rows, innerW, ok := m.activePreview()
	if !ok {
		return m, nil, false
	}
	if p.cs.on && p.previewCharStale() {
		p.previewCharLeave()
	}
	p.pageH = rows
	if !p.cs.on {
		if msg.String() != "v" || p.search.typing {
			return m, nil, false
		}
		if p.mode == modeCutoff { // a cut line hides the cursor (and reads "…"): scroll while on
			p.csMode, p.csSwapped, p.mode, p.hscroll = p.mode, true, modeScroll, 0
			if d := m.previewDoc(p); d != nil && d.ov != nil {
				d.layOut(rows, d.ov.w) // an overview lays itself out: re-lay it for the view now
			}
		}
		p.csRows = make([]charRow, len(p.lines))
		for i, l := range p.lines {
			if !l.src || l.cells != nil {
				p.csRows[i] = charRow{dead: true}
				continue
			}
			p.csRows[i] = charRow{text: previewCharText(l), wraps: l.cont, hard: l.cont && l.hard}
		}
		if !p.cs.enter(p.csRows, pos{row: p.cur}) {
			p.previewCharLeave() // the view back
			m.statusMsg = i18n.T("▸ nothing to select")
			return m, nil, true
		}
		p.lsel.clear() // one selection kind at a time
		p.csLines = p.lines
		p.cur = p.cs.cur.row
		p.ensureCursorVisible(rows)
		return m, nil, true
	}
	res := charSelKey(&p.cs, p, msg)
	if !res.handled {
		return m, nil, false
	}
	if p.cs.on {
		p.cur = p.cs.cur.row // the line cursor (and the pager) follows
		p.ensureCursorVisible(rows)
		if p.mode == modeScroll {
			lead := min(max(p.lines[p.cur].lead, 0), len([]rune(p.lines[p.cur].raw)))
			text := p.csRows[p.cur].text
			off := charPanCells(string(text), p.cs.cur.col, max(p.hscroll-lead, 0), max(innerW-lead, 1), sanitizeForDisplay)
			p.hscroll = off + lead
			if p.cs.cur.col == 0 && lead > 0 && p.hscroll <= lead {
				p.hscroll = 0
			}
		}
	} else {
		p.previewCharLeave()
	}
	if res.notice != "" {
		m.statusMsg = res.notice
	}
	if res.copy != "" {
		return m, m.copyToClipboardCmd(copiedCharsText(res.count), res.copy), true
	}
	return m, nil, true
}
