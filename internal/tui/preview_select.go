package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// activePreview is THE focused file preview and its on-screen size: the one
// question every preview key, the line cursor and the . menu's line rows ask.
// A fileViewer on top of the stack answers first; otherwise the files view's
// right-column preview answers while it is focused (the tree side owns the
// keys otherwise).
func (m Model) activePreview() (p *contentPopup, rows, innerW int, ok bool) {
	if fv, isViewer := m.topLayer().(*fileViewer); isViewer {
		rows, innerW = fv.geom(m)
		return fv.p, rows, innerW, true
	}
	if m.filesPreview != nil && !m.filesTreeFocused {
		return m.filesPreview.p, m.filePreviewRowsCap(), m.filePreviewInnerW(), true
	}
	return nil, 0, 0, false
}

// ensureCursorVisible scrolls the pager's top line (sel) the minimum needed for
// cur to sit inside a rowsCap-row window — the diff's j/k rule. In wrap mode a
// row can occupy several display lines, so rowsCap is an upper bound there and
// the cursor may end up one row short of the bottom; that is the same
// approximation snapHit already makes, and previewClamp keeps the top legal.
func (p *contentPopup) ensureCursorVisible(rowsCap int) {
	if rowsCap < 1 {
		rowsCap = 1
	}
	if p.cur < p.sel {
		p.sel = p.cur
	}
	if p.cur >= p.sel+rowsCap {
		p.sel = p.cur - rowsCap + 1
	}
	p.sel = previewClamp(p.sel, len(p.lines), rowsCap, p.mode)
}

// movePreviewCursor steps the focused preview's line cursor by delta, clamps it
// to the file and scrolls minimally to keep it on screen. A cursor the user
// scrolled OUT of the window (↑/↓, the wheel and the page keys move only the
// pager) does not drag the viewport back to wherever it was: the first alt+↑/↓
// re-enters it at the nearest edge of the window — the top row when it was
// above, the bottom row when it was below — and the pager top stays put. The
// Model is a value but filesPreview is a pointer, so the mutation is visible
// to the caller.
func (m Model) movePreviewCursor(delta int) {
	p, rowsCap, _, ok := m.activePreview()
	if !ok || len(p.lines) == 0 {
		return
	}
	if p.cur < p.sel {
		p.cur = p.sel
		return
	}
	if p.cur >= p.sel+rowsCap {
		p.cur = p.sel + rowsCap - 1
		if p.cur > len(p.lines)-1 {
			p.cur = len(p.lines) - 1
		}
		// Wrap mode shows fewer than rowsCap rows when lines wrap, so the
		// "bottom row" may sit one row low there — the same approximation
		// ensureCursorVisible makes; it keeps the top legal either way.
		p.ensureCursorVisible(rowsCap)
		return
	}
	p.cur += delta
	if p.cur < 0 {
		p.cur = 0
	}
	if p.cur > len(p.lines)-1 {
		p.cur = len(p.lines) - 1
	}
	p.ensureCursorVisible(rowsCap)
}

// selectedLines is the SOURCE text of every preview line the selection covers.
// Placeholder lines carry src false and are skipped — they are not lines of the
// file. An EMPTY source line is kept: it is a line.
func (p *contentPopup) selectedLines() []string {
	lo, hi, ok := p.lsel.bounds(p.cur)
	if !ok {
		return nil
	}
	if lo < 0 {
		lo = 0
	}
	if hi > len(p.lines)-1 {
		hi = len(p.lines) - 1
	}
	var out []string
	for i := lo; i <= hi; i++ {
		if !p.lines[i].src {
			continue
		}
		out = append(out, p.lines[i].raw)
	}
	return out
}

// previewCopyLineRows are the . menu's line-copy rows for a FOCUSED View-file
// preview. Copy line is offered only on a real source line; a placeholder
// ("(loading…)") is not a line of the file and has nothing to copy.
func (m Model) previewCopyLineRows() []actionRow {
	p, _, _, ok := m.activePreview()
	if !ok {
		return nil
	}
	var rows []actionRow
	if p.cur >= 0 && p.cur < len(p.lines) && p.lines[p.cur].src {
		rows = append(rows, m.copyRow("copy-line", i18n.T("Copy line"),
			i18n.T("Copied line %d", p.cur+1), p.lines[p.cur].raw))
	}
	if sel := p.selectedLines(); len(sel) > 0 {
		rows = append(rows, clearingCopyRow(m.copyRow("copy-selected-lines",
			i18n.T("Copy selected lines (%d)", len(sel)),
			i18n.T("Copied %d lines", len(sel)),
			strings.Join(sel, "\n")), func(m Model) *lineSel {
			if pp, _, _, ok := m.activePreview(); ok {
				return &pp.lsel
			}
			return nil
		}))
	}
	return rows
}

// previewSelectKey gives the preview's line selection its shot at a key, BEFORE
// previewSearchKey — see diffSelectKey for why that order is the spec's. enter
// WITHOUT a selection keeps its old meaning (focus moves to the tree), so the
// hook declines it.
func (m Model) previewSelectKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	p, _, _, ok := m.activePreview()
	if !ok || p.search.typing {
		return m, nil, false
	}
	switch msg.String() {
	case " ":
		// The preview owns the keyboard, so the key is consumed either way —
		// but a placeholder line cannot start a range.
		if p.cur < 0 || p.cur >= len(p.lines) || !p.lines[p.cur].src {
			return m, nil, true
		}
		p.lsel.press(p.cur)
		return m, nil, true
	case "enter":
		if !p.lsel.on {
			return m, nil, false
		}
		row, ok := rowByID(m.previewCopyLineRows(), "copy-selected-lines")
		p.lsel.clear()
		if !ok {
			return m, nil, true
		}
		nm, cmd := row.run(m)
		return nm.(Model), cmd, true
	case "esc":
		if !p.lsel.on {
			return m, nil, false
		}
		p.lsel.clear()
		return m, nil, true
	}
	return m, nil, false
}
