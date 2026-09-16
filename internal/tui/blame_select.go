package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// selectedLines is the SOURCE content of every blame line the selection covers.
// Blame has no absent cells and no folds — every row is a line — so the only
// filtering is the clamp to the slice.
func (b *blameView) selectedLines() []string {
	lo, hi, ok := b.lsel.bounds(b.sel)
	if !ok {
		return nil
	}
	if lo < 0 {
		lo = 0
	}
	if hi > len(b.lines)-1 {
		hi = len(b.lines) - 1
	}
	var out []string
	for i := lo; i <= hi; i++ {
		out = append(out, b.lines[i].Content)
	}
	return out
}

// blameCopyLineRows are the . menu's line-copy rows for the blame view: Copy
// line is always offered (every blame row IS a line of the file), and Copy
// selected lines (N) joins it while a range is live. Both carry their payload
// as copyText, and the enter key runs the second row itself.
func (m Model) blameCopyLineRows(b *blameView) []actionRow {
	if b == nil || b.sel < 0 || b.sel >= len(b.lines) {
		return nil
	}
	ln := b.lines[b.sel]
	rows := []actionRow{
		m.copyRow("copy-line", i18n.T("Copy line"), i18n.T("Copied line %d", ln.LineNo), ln.Content),
	}
	if sel := b.selectedLines(); len(sel) > 0 {
		rows = append(rows, m.copyRow("copy-selected-lines",
			i18n.T("Copy selected lines (%d)", len(sel)),
			i18n.T("Copied %d lines", len(sel)),
			strings.Join(sel, "\n")))
	}
	return rows
}

// blameSelectKey gives the line selection its shot at a key, BEFORE the search
// hook — see diffSelectKey for why that order is the one the spec asks for.
// enter WITHOUT a selection keeps its own meaning (the history of the commit
// under the cursor), so the hook declines it; b is never two-stage and never
// reaches here.
func (m Model) blameSelectKey(b *blameView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if b == nil || b.search.typing {
		return m, nil, false
	}
	switch msg.String() {
	case " ":
		if len(b.lines) == 0 {
			return m, nil, true
		}
		b.lsel.press(b.sel)
		return m, nil, true
	case "enter":
		if !b.lsel.on {
			return m, nil, false
		}
		row, ok := rowByID(m.blameCopyLineRows(b), "copy-selected-lines")
		b.lsel.clear()
		if !ok {
			return m, nil, true
		}
		nm, cmd := row.run(m)
		return nm.(Model), cmd, true
	case "esc":
		if !b.lsel.on {
			return m, nil, false
		}
		b.lsel.clear()
		return m, nil, true
	}
	return m, nil, false
}
