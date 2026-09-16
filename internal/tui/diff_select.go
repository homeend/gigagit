package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// lockedSideNotice names the side a live selection is pinned to. alt+←/→ post
// it instead of flipping: the range copies ONE side's text, so the lock is the
// contract, not a limitation — and the notice names the way out (esc).
func lockedSideNotice(onOld bool) string {
	if onOld {
		return i18n.T("▸ selection locked to the old side — esc clears it")
	}
	return i18n.T("▸ selection locked to the new side — esc clears it")
}

// diffCopyLineRows are the . menu's line-copy rows for the diff view on top:
// "Copy line" for the cursor cell (named when the cursor sits on the OLD side,
// where the text is not what the file says today) and "Copy selected lines (N)"
// while a range holds N copyable lines. Both carry their payload as copyText,
// captured at menu-build time, and the enter key runs the second row itself —
// so the key and the menu can never disagree.
//
// Gated exactly like diffEditRow: a loading / failed / binary / too-large view
// has no text to copy. A gap cell offers no "Copy line" at all.
func (m Model) diffCopyLineRows() []actionRow {
	v, ok := m.topLayer().(*diffView)
	if !ok || v.loading || v.err != nil || v.binary || v.tooLarge {
		return nil
	}
	var rows []actionRow
	if text, no, ok := v.cursorCell(); ok {
		label := i18n.T("Copy line")
		if v.onOld {
			label = i18n.T("Copy line (old side)")
		}
		rows = append(rows, m.copyRow("copy-line", label, i18n.T("Copied line %d", no), text))
	}
	if sel := v.selectedLines(); len(sel) > 0 {
		rows = append(rows, clearingCopyRow(m.copyRow("copy-selected-lines",
			i18n.T("Copy selected lines (%d)", len(sel)),
			i18n.T("Copied %d lines", len(sel)),
			strings.Join(sel, "\n")), func(m Model) *lineSel {
			if d := m.diffLayer(); d != nil {
				return &d.lsel
			}
			return nil
		}))
	}
	return rows
}

// diffSelectKey gives the line selection its shot at a key, BEFORE the search
// hook. The order matters and is the spec's: a search being TYPED owns every
// key (so space is a space in the query), then the selection, then a committed
// query, then the view's own esc. Since diffSearchKey handles the typing case
// and the committed-query case in one call, the only way to sit between them is
// to run first and decline while typing.
//
// handled == true means the caller must return immediately.
func (m Model) diffSelectKey(v *diffView, msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if v == nil || v.search.typing {
		return m, nil, false
	}
	switch msg.String() {
	case " ":
		// The diff view owns the keyboard, so the key is consumed either way —
		// but a view with nothing to select cannot start a range. The gate is
		// diffCopyLineRows': a loading, errored, binary or too-large view has
		// no lines and offers no copy rows, so a range there would show the
		// selection footer and copy nothing. Consuming without a state change
		// is what blame does on an empty file and the preview on a placeholder.
		if v.loading || v.err != nil || v.binary || v.tooLarge {
			return m, nil, true
		}
		v.lsel.press(v.curLine)
		return m, nil, true
	case "enter":
		// enter has no other meaning in the diff view, so it stays unbound
		// without a selection rather than growing one here.
		if !v.lsel.on {
			return m, nil, false
		}
		// Resolve the row BEFORE clearing: it reads the live range.
		row, ok := rowByID(m.diffCopyLineRows(), "copy-selected-lines")
		v.lsel.clear()
		if !ok {
			m.diffNotice = i18n.T("▸ nothing to copy on this side")
			return m, nil, true
		}
		nm, cmd := row.run(m)
		return nm.(Model), cmd, true
	case "esc":
		if !v.lsel.on {
			return m, nil, false
		}
		v.lsel.clear()
		return m, nil, true
	}
	return m, nil, false
}

// diffSelectHint is the footer while a selection is live: it replaces the whole
// base hint, because every key that matters in that state is here and the user
// must always be able to see the way out (esc). Mode-independent, so one key
// serves the three long-line variants; measured at 86 columns, well inside the
// 140 budget.
func diffSelectHint() string {
	return i18n.T("[space] mark end  [enter] copy  [esc] unmark  [alt+↑↓/jk] extend  [alt↔] side (locked)")
}
