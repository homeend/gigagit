package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/textdiff"
)

// The diff view's line cursor. curLine indexes v.lines (the logical stream:
// one entry per aligned row, plus fold separators in partial mode), never
// v.disp — a wrapped line owns several display rows, and lineStart maps the
// cursor to its first one, so wrap toggles and resizes need no bookkeeping.
// The cursor never rests on a fold; every mover snaps off one. The cursor
// and the focused change block (cur) are independent: n/p seed the cursor
// through focusBlock, but a free scroll (arrows, wheel) moves neither.
//
// cursorRow is the contract later phases build on: review notes anchor on
// the row under the cursor (RightNo on the new side, LeftNo on a Del row).

// cursorAlign is a z-cycle position for the cursor line in the viewport.
// Declaration order is the cycle order (Emacs recenter: center, top, bottom).
type cursorAlign int

const (
	alignCenter cursorAlign = iota
	alignTop
	alignBottom
)

// cursorRow returns the aligned row under the cursor, or false when the view
// has no lines or the cursor index is out of range.
func (v *diffView) cursorRow() (textdiff.Row, bool) {
	if v.curLine < 0 || v.curLine >= len(v.lines) || v.lines[v.curLine].Fold > 0 {
		return textdiff.Row{}, false
	}
	return v.lines[v.curLine].Row, true
}

// cursorDispRange is the half-open display-row range the cursor line owns
// (one row, or several when wrapped). (0,0) when there is no cursor.
func (v *diffView) cursorDispRange() (start, end int) {
	if v.curLine < 0 || v.curLine >= len(v.lineStart) {
		return 0, 0
	}
	start = v.lineStart[v.curLine]
	end = len(v.disp)
	if v.curLine+1 < len(v.lineStart) {
		end = v.lineStart[v.curLine+1]
	}
	// The cursor marks the CONTENT rows of its line only: the note rows that
	// follow belong to the line but are not part of it.
	for i := start; i < end && i < len(v.disp); i++ {
		if v.disp[i].note != nil {
			end = i
			break
		}
	}
	return start, end
}

// snapOffFold moves li forward, then backward, to the nearest non-fold line.
// Returns -1 when the stream has no real line at all.
func (v *diffView) snapOffFold(li int) int {
	n := len(v.lines)
	if n == 0 {
		return -1
	}
	if li < 0 {
		li = 0
	}
	if li > n-1 {
		li = n - 1
	}
	for j := li; j < n; j++ {
		if v.lines[j].Fold == 0 {
			return j
		}
	}
	for j := li - 1; j >= 0; j-- {
		if v.lines[j].Fold == 0 {
			return j
		}
	}
	return -1
}

// setCursorLine puts the cursor on logical line li (snapped off a fold) and
// scrolls minimally so it is visible.
func (v *diffView) setCursorLine(li, body int) {
	if j := v.snapOffFold(li); j >= 0 {
		v.curLine = j
	} else {
		v.curLine = 0
	}
	v.ensureCursorVisible(body)
}

// moveCursor steps delta real lines (a fold counts as no line) and clamps at
// the ends, then scrolls minimally.
func (v *diffView) moveCursor(delta, body int) {
	li := v.snapOffFold(v.curLine)
	if li < 0 {
		return
	}
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	for ; delta > 0; delta-- {
		j := li + step
		for j >= 0 && j < len(v.lines) && v.lines[j].Fold > 0 {
			j += step
		}
		if j < 0 || j >= len(v.lines) {
			break
		}
		li = j
	}
	v.curLine = li
	v.ensureCursorVisible(body)
}

// setCursorDisp moves the cursor to the line that owns display row `row`
// (a click). A fold row or an out-of-range row leaves the cursor alone.
func (v *diffView) setCursorDisp(row, body int) {
	if row < 0 || row >= len(v.disp) || v.disp[row].fold > 0 {
		return
	}
	v.curLine = v.disp[row].line
	v.ensureCursorVisible(body)
}

// pageCursor is the page keys' cursor move: one body of DISPLAY rows, not of
// logical lines. In wrap mode a line owns several display rows, so stepping
// `body` LINES would run the cursor (and, through ensureCursorVisible, the
// viewport) far past the one page scrollBy just made. The target display row
// is clamped into the stream and mapped to its owning line; a fold row steps
// to the nearest real row in the direction of travel, falling back to the
// other direction, so the key is never a no-op.
func (v *diffView) pageCursor(delta, body int) {
	if len(v.disp) == 0 || v.curLine < 0 || v.curLine >= len(v.lineStart) {
		return
	}
	row := v.lineStart[v.curLine] + delta
	if row < 0 {
		row = 0
	}
	if row > len(v.disp)-1 {
		row = len(v.disp) - 1
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	i := row
	for i >= 0 && i < len(v.disp) && v.disp[i].fold > 0 {
		i += step
	}
	if i < 0 || i >= len(v.disp) {
		for i = row; i >= 0 && i < len(v.disp) && v.disp[i].fold > 0; i -= step {
		}
	}
	if i < 0 || i >= len(v.disp) {
		return // nothing but folds
	}
	v.curLine = v.disp[i].line
	v.ensureCursorVisible(body)
}

// ensureCursorVisible scrolls the least amount that brings the cursor line's
// first display row inside [offset, offset+body).
func (v *diffView) ensureCursorVisible(body int) {
	if len(v.disp) == 0 {
		return
	}
	start, _ := v.cursorDispRange()
	switch {
	case start < v.offset:
		v.offset = start
	case start >= v.offset+body:
		v.offset = start - body + 1
	}
	v.scroll(0, body) // clamp
}

// alignCursor places the cursor line's first display row at the top, the
// centre, or the bottom of the body (clamped into the scroll range).
func (v *diffView) alignCursor(mode cursorAlign, body int) {
	start, _ := v.cursorDispRange()
	switch mode {
	case alignTop:
		v.offset = start
	case alignBottom:
		v.offset = start - body + 1
	default:
		v.offset = start - body/2
	}
	v.scroll(0, body) // clamp
}

// reanchorCursor re-finds the cursor after a rebuild (f / ctrl+w) by the
// row's source numbers: the first line with the same (LeftNo, RightNo). A row
// hidden by a fold, or gone, leaves the index clamped and snapped off a fold.
func (v *diffView) reanchorCursor(leftNo, rightNo int) {
	for i, ln := range v.lines {
		if ln.Fold == 0 && ln.Row.LeftNo == leftNo && ln.Row.RightNo == rightNo {
			v.curLine = i
			return
		}
	}
	if j := v.snapOffFold(v.curLine); j >= 0 {
		v.curLine = j
	} else {
		v.curLine = 0
	}
}

// cursorVisible reports whether the cursor line's first display row is inside
// the body window. Read BEFORE a rebuild, it says whether the user could see
// the cursor at all.
func (v *diffView) cursorVisible(body int) bool {
	if len(v.disp) == 0 {
		return false
	}
	start, _ := v.cursorDispRange()
	return start >= v.offset && start < v.offset+body
}

// reanchorAfterRebuild restores the cursor after f / ctrl+w rebuilt the
// streams: the row is re-found by its source numbers (focusBlock has meanwhile
// re-seeded curLine to the focused block's first row, which is not where the
// user left the cursor). The viewport is only pulled back to the cursor when
// the cursor was VISIBLE before the toggle — otherwise the user was looking
// somewhere else entirely (arrows and the wheel scroll the viewport without
// moving the cursor), and focusBlock has already put the view on the change
// they were reading; dragging it back to a stale off-screen cursor loses it.
func (v *diffView) reanchorAfterRebuild(cr textdiff.Row, hadRow, wasVisible bool, body int) {
	if !hadRow {
		return // no row to re-find: focusBlock's seeding stands
	}
	v.reanchorCursor(cr.LeftNo, cr.RightNo)
	if wasVisible {
		v.ensureCursorVisible(body)
	}
}

// cursorStyle is the effective marker style: the session override, else config.
func (m Model) cursorStyle() string {
	if m.diffCursor != "" {
		return m.diffCursor
	}
	return m.cfg.UI.CursorStyle()
}

// nextCursorStyle is the . menu's cycle: row → number → off → row.
func nextCursorStyle(s string) string {
	switch s {
	case "row":
		return "number"
	case "number":
		return "off"
	}
	return "row"
}

// cursorStyleLabel is the human name of a marker style for the menu row.
func cursorStyleLabel(s string) string {
	switch s {
	case "number":
		return i18n.T("number")
	case "off":
		return i18n.T("off")
	}
	return i18n.T("row")
}

// diffAlignRows are the . menu's three viewport alignments for the cursor
// line (the z key cycles them).
func (m Model) diffAlignRows() []actionRow {
	if _, ok := m.topLayer().(*diffView); !ok {
		return nil
	}
	mk := func(id, label string, mode cursorAlign) actionRow {
		return actionRow{id: id, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			if v := m.diffLayer(); v != nil {
				v.alignCursor(mode, m.diffBodyRows())
				v.zCycle = (mode + 1) % 3
			}
			return m, nil
		}}
	}
	return []actionRow{
		mk("diff-align-top", i18n.T("Align cursor line: top"), alignTop),
		mk("diff-align-center", i18n.T("Align cursor line: center"), alignCenter),
		mk("diff-align-bottom", i18n.T("Align cursor line: bottom"), alignBottom),
	}
}

// diffCursorStyleRow is the . menu row that cycles the marker style for the
// session. Only while a diff view is on top.
func (m Model) diffCursorStyleRow() (actionRow, bool) {
	if _, ok := m.topLayer().(*diffView); !ok {
		return actionRow{}, false
	}
	cur := m.cursorStyle()
	return actionRow{
		id:    "diff-cursor-style",
		label: i18n.T("Cursor marker: %s", cursorStyleLabel(cur)),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.diffCursor = nextCursorStyle(cur)
			return m, nil
		},
	}, true
}
