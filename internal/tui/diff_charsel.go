package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/syntax"
)

// The diff view as a character-selection host (charsel.go): the rows are
// the cursor side's lines — stacked, one element's — a row dead where the
// side has no cell or the line is a fold, a header, a gap. Column math is
// in SOURCE runes (a tab is one rune, copied as a tab); the painter maps a
// column to the sanitized cell through dispCol.

// charRange is the lines the mode works in: the whole stream for a single
// file, the element (csFile) for a stack.
func (v *diffView) charRange() (lo, hi int) {
	if v.stk == nil {
		return 0, len(v.lines) - 1
	}
	return v.fileLineRange(v.csFile)
}

func (v *diffView) charRows() []charRow {
	lo, hi := v.charRange()
	if lo < 0 || hi < lo || hi >= len(v.lines) {
		return nil
	}
	out := make([]charRow, 0, hi-lo+1)
	for li := lo; li <= hi; li++ {
		ln := v.lines[li]
		switch {
		case ln.kind == lineProse: // the stack's summary element
			if f, ok := v.stackFileAt(li); ok && ln.prose >= 0 && ln.prose < len(f.prose) {
				out = append(out, proseCharRow(f.prose[ln.prose]))
				continue
			}
			out = append(out, charRow{dead: true})
		case ln.isBody() && sidePresent(ln.Row, v.onOld):
			out = append(out, charRow{text: []rune(sideText(ln.Row, v.onOld))})
		default:
			out = append(out, charRow{dead: true})
		}
	}
	return out
}

func (v *diffView) charPage() int { return max(v.csPage, 1) }

// dispCol is the sanitized-cell rune index of source column col of text:
// the same expansion sanitizeCell applies (a tab to the next 4-column stop,
// a control rune to one cell, every other rune to one). A column past the
// text's end is one cell per rune it is past: the empty row's cursor cell.
func dispCol(text string, col int) int {
	idx, c := 0, 0
	runes := []rune(text)
	if col > len(runes) {
		idx += col - len(runes)
	}
	for i, r := range runes {
		if i >= col {
			break
		}
		switch {
		case r == '\t':
			n := 4 - c%4
			idx += n
			c += n
		default:
			idx++
			c++
		}
	}
	return idx
}

// charSpansOn is the selection on logical line li as spans in the sanitized
// cell's rune space (what the cell painters overlay); nil when the mode is
// off or the line is outside its element.
func (v *diffView) charSpansOn(li int) []hitSpan {
	if !v.cs.on {
		return nil
	}
	lo, hi := v.charRange()
	if li < lo || li > hi {
		return nil
	}
	ln := v.lines[li]
	if !ln.isBody() || !sidePresent(ln.Row, v.onOld) {
		return nil
	}
	return charSpansDisp(v.cs, li-lo, sideText(ln.Row, v.onOld))
}

// roomForCursor is an empty wrap segment as one blank cell: the cursor (or
// the stripe) of the character selection needs a cell to paint on.
func roomForCursor(seg cellSeg) cellSeg {
	if len(seg.disp) > 0 {
		return seg
	}
	seg.disp, seg.emph, seg.cls = []rune{' '}, make([]emphLevel, 1), make([]syntax.Class, 1)
	return seg
}

// proseCharRow is a rendered markdown row as the mode's row: its layout
// lead (a list item's hang indent on a continuation) stripped, the join
// rule from the wrapper.
func proseCharRow(r mdRow) charRow {
	text := []rune(r.text)
	lead := min(max(r.lead, 0), len(text))
	return charRow{text: text[lead:], wraps: r.cont, hard: r.hard}
}

// charEmphProse is the mask of a stack's prose row (n = its display runes):
// the mode's mask placed behind the row's layout lead.
func (v *diffView) charEmphProse(li, n int) []emphLevel {
	if !v.cs.on {
		return nil
	}
	lo, hi := v.charRange()
	if li < lo || li > hi {
		return nil
	}
	lead := 0
	if f, ok := v.stackFileAt(li); ok && v.lines[li].kind == lineProse && v.lines[li].prose >= 0 && v.lines[li].prose < len(f.prose) {
		lead = min(max(f.prose[v.lines[li].prose].lead, 0), n)
	}
	return shiftMask(charSelEmph(v.cs, li-lo, n-lead), lead, n)
}

// charPaneText is a pane's text width in scroll mode: the pane less its
// gutter and the separator column (diffPaneLines' arithmetic).
func (v *diffView) charPaneText(m Model) int {
	w, _ := m.overlayDims()
	paneW := (w - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	return max(paneW-v.gutter()-1, 1)
}

// charPan keeps the cursor's display column inside the pane in scroll mode
// (←/→ are the mode's own keys, so nothing else could pan).
func (v *diffView) charPan(m Model) {
	if v.long != longScroll || !v.cs.on {
		return
	}
	rows := v.charRows()
	if v.cs.cur.row < 0 || v.cs.cur.row >= len(rows) {
		return
	}
	col := dispCol(string(rows[v.cs.cur.row].text), v.cs.cur.col)
	tw := v.charPaneText(m)
	switch {
	case col < v.hOffset:
		v.hOffset = col
	case col >= v.hOffset+tw:
		v.hOffset = col - tw + 1
	}
	if v.hOffset < 0 {
		v.hOffset = 0
	}
}

// diffCharEnter is v: the mode on at the cursor line, column 0 — stacked,
// within the cursor's element. false with nothing to select.
func (m Model) diffCharEnter(v *diffView) bool {
	v.lsel.clear() // one selection kind at a time
	v.csBase, v.csFile = 0, 0
	if v.stk != nil {
		v.csFile = v.lines[v.curLine].file
		v.csBase, _ = v.fileLineRange(v.csFile)
	}
	v.csPage = m.diffBodyRows()
	if !v.cs.enter(v.charRows(), pos{row: v.curLine - v.csBase}) {
		return false
	}
	if v.long == longTruncate { // a cut line hides the cursor: scroll while on
		v.csLong, v.csSwapped, v.long, v.hOffset = v.long, true, longScroll, 0
		v.relayout(v.width)
	}
	v.setCursorLine(v.csBase+v.cs.cur.row, m.diffBodyRows()) // a skipped row (a header): the line cursor lands with it
	v.charPan(m)                                             // a view panned right before v shows the cursor at once
	return true
}

// charEnded puts back the long-line mode the character selection swapped
// away (truncate → scroll while on) once the mode is off; it reports whether
// it did — the caller relays out.
func (v *diffView) charEnded() bool {
	if v.cs.on || !v.csSwapped {
		return false
	}
	v.long, v.csSwapped, v.hOffset = v.csLong, false, 0
	return true
}

// diffCharKey gives the character selection every key while it is on, and
// v when it is off. It runs BEFORE the line selection and the search hooks
// (updateDiffViewKey): the mode is modal, nothing else may see a key.
// handled == true means the caller must return immediately.
func (m Model) diffCharKey(v *diffView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if v == nil {
		return m, nil, false
	}
	if !v.cs.on {
		if msg.String() != "v" || v.search.typing {
			return m, nil, false
		}
		if v.loading || v.err != nil || v.binary || v.tooLarge || len(v.lines) == 0 {
			m.diffNotice = i18n.T("▸ nothing to select")
			return m, nil, true
		}
		if !m.diffCharEnter(v) {
			m.diffNotice = i18n.T("▸ nothing to select")
		}
		return m, nil, true
	}
	v.csPage = m.diffBodyRows()
	res := charSelKey(&v.cs, v, msg)
	if !res.handled {
		return m, nil, false
	}
	if v.cs.on {
		v.setCursorLine(v.csBase+v.cs.cur.row, m.diffBodyRows()) // the view scrolls with the cursor
		v.charPan(m)
	} else if v.charEnded() {
		v.relayout(v.width)
	}
	if res.notice != "" {
		m.diffNotice = res.notice
	}
	if res.bump && v.stk != nil {
		m.diffNotice = m.stackCharBoundNotice(v)
	}
	if res.copy != "" {
		return m, m.copyToClipboardCmd(copiedCharsText(res.count), res.copy), true
	}
	return m, nil, true
}

// stackCharBoundNotice says what bounds the selection in a stack: the
// element (the summary, a shelved set's note) or the file.
func (m Model) stackCharBoundNotice(v *diffView) string {
	if v.stk != nil && v.csFile >= 0 && v.csFile < len(v.stk.files) {
		if f := v.stk.files[v.csFile]; f.summary || f.label != "" {
			return i18n.T("▸ the selection stays in this element")
		}
	}
	return i18n.T("▸ the selection stays in this file")
}
