package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/textdiff"
)

// cursorRows builds n Same rows numbered 1..n with the listed rows Changed —
// the same shape as sameRowsTUI, local so this file reads on its own.
func cursorRows(n int, changed ...int) []textdiff.Row {
	rows := make([]textdiff.Row, n)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: "l", Right: "r", LeftNo: i + 1, RightNo: i + 1}
	}
	for _, c := range changed {
		rows[c] = textdiff.Row{Kind: textdiff.Changed, Left: "x", Right: "y", LeftNo: c + 1, RightNo: c + 1}
	}
	return rows
}

func TestCursorMoveClampsAndScrolls(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40), nil)
	body := 10
	if v.curLine != 0 {
		t.Fatalf("fresh view curLine = %d, want 0", v.curLine)
	}
	v.moveCursor(-1, body)
	if v.curLine != 0 || v.offset != 0 {
		t.Fatalf("k at top: curLine=%d offset=%d, want 0/0", v.curLine, v.offset)
	}
	for i := 0; i < 12; i++ {
		v.moveCursor(1, body)
	}
	// Cursor on line 12; the viewport must have scrolled minimally: last body row.
	if v.curLine != 12 || v.offset != 3 {
		t.Fatalf("12×j: curLine=%d offset=%d, want 12/3", v.curLine, v.offset)
	}
	v.moveCursor(100, body)
	if v.curLine != 39 || v.offset != 30 {
		t.Fatalf("j past end: curLine=%d offset=%d, want 39/30", v.curLine, v.offset)
	}
	r, ok := v.cursorRow()
	if !ok || r.RightNo != 40 {
		t.Fatalf("cursorRow = %+v %v, want RightNo 40", r, ok)
	}
}

func TestCursorSkipsFoldsInPartialMode(t *testing.T) {
	t.Parallel()
	// 40 rows, changes at 10 and 30: partial mode keeps diffContext rows
	// around each and folds the rest, so a fold sits BETWEEN two real lines
	// (the very first line is a fold too; the test needs a middle one).
	v := diffViewWith(cursorRows(40, 10, 30), []int{10, 30})
	v.partial = true
	v.rebuild()
	body := 10
	foldAt := -1
	for i, ln := range v.lines {
		if ln.Fold > 0 && i > 0 {
			foldAt = i
			break
		}
	}
	if foldAt < 0 || foldAt+1 >= len(v.lines) {
		t.Fatal("fixture: expected a middle fold line in partial mode")
	}
	v.setCursorLine(foldAt, body)
	if v.lines[v.curLine].Fold > 0 {
		t.Fatalf("setCursorLine on a fold must snap to a real row, got line %d", v.curLine)
	}
	// Step over the fold both ways: the cursor never rests on it.
	v.setCursorLine(foldAt-1, body)
	v.moveCursor(1, body)
	if v.curLine != foldAt+1 {
		t.Fatalf("j over a fold: curLine=%d, want %d", v.curLine, foldAt+1)
	}
	v.moveCursor(-1, body)
	if v.curLine != foldAt-1 {
		t.Fatalf("k over a fold: curLine=%d, want %d", v.curLine, foldAt-1)
	}
}

func TestCursorAlign(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(60), nil)
	body := 10
	v.setCursorLine(30, body)
	v.alignCursor(alignTop, body)
	if v.offset != 30 {
		t.Fatalf("top: offset=%d, want 30", v.offset)
	}
	v.alignCursor(alignCenter, body)
	if v.offset != 25 {
		t.Fatalf("center: offset=%d, want 25", v.offset)
	}
	v.alignCursor(alignBottom, body)
	if v.offset != 21 {
		t.Fatalf("bottom: offset=%d, want 21", v.offset)
	}
	// Clamped at the ends: line 2 can't be centered below offset 0, the last
	// line can't be top-anchored past len-body.
	v.setCursorLine(2, body)
	v.alignCursor(alignCenter, body)
	if v.offset != 0 {
		t.Fatalf("center near top: offset=%d, want 0", v.offset)
	}
	v.setCursorLine(59, body)
	v.alignCursor(alignTop, body)
	if v.offset != 50 {
		t.Fatalf("top near end: offset=%d, want 50", v.offset)
	}
}

func TestCursorDispRangeSpansWrappedLine(t *testing.T) {
	t.Parallel()
	rows := cursorRows(5)
	long := ""
	for i := 0; i < 30; i++ {
		long += "word "
	}
	rows[2].Left, rows[2].Right = long, long // wraps to several display rows in a 40-col view
	v := diffViewWith(rows, nil)
	v.long = longWrap
	v.relayout(40)
	v.setCursorLine(2, 20)
	s, e := v.cursorDispRange()
	if s != v.lineStart[2] || e-s < 2 {
		t.Fatalf("range = [%d,%d) lineStart=%d, want the wrapped line's full span", s, e, v.lineStart[2])
	}
	if v.lineStart[3] != e {
		t.Fatalf("range end %d must equal the next line's first display row %d", e, v.lineStart[3])
	}
}

func TestCursorReanchorsAcrossPartialToggle(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40, 20, 30), []int{20, 30})
	body := 10
	v.setCursorLine(30, body) // the second change, row RightNo 31
	before, _ := v.cursorRow()
	v.partial = true
	v.rebuild()
	v.reanchorCursor(before.LeftNo, before.RightNo)
	after, ok := v.cursorRow()
	if !ok || after.LeftNo != before.LeftNo || after.RightNo != before.RightNo {
		t.Fatalf("after partial toggle cursorRow = %+v, want %+v", after, before)
	}
	v.partial = false
	v.rebuild()
	v.reanchorCursor(before.LeftNo, before.RightNo)
	if v.curLine != 30 {
		t.Fatalf("back to full: curLine=%d, want 30", v.curLine)
	}
}

func TestCursorSetDispIgnoresFoldRow(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40, 10, 30), []int{10, 30})
	v.partial = true
	v.rebuild()
	foldRow := -1
	for i, d := range v.disp {
		if d.fold > 0 && i > 0 { // a middle fold (display row 0 is a fold as well)
			foldRow = i
			break
		}
	}
	if foldRow < 0 || foldRow+1 >= len(v.disp) {
		t.Fatal("fixture: expected a middle fold display row")
	}
	v.setCursorLine(1, 10)
	was := v.curLine
	v.setCursorDisp(foldRow, 10)
	if v.curLine != was {
		t.Fatalf("click on a fold row moved the cursor to %d, want %d", v.curLine, was)
	}
	v.setCursorDisp(foldRow+1, 10)
	if v.curLine != v.disp[foldRow+1].line {
		t.Fatalf("click on row %d: curLine=%d, want %d", foldRow+1, v.curLine, v.disp[foldRow+1].line)
	}
}

func TestFocusBlockSeedsCursor(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40, 20, 30), []int{20, 30})
	v.focusBlock(1, 10)
	if v.curLine != 30 {
		t.Fatalf("focusBlock(1): curLine=%d, want 30", v.curLine)
	}
	v.focusBlock(0, 10)
	if v.curLine != 20 {
		t.Fatalf("focusBlock(0): curLine=%d, want 20", v.curLine)
	}
}

func TestDiffKeysJKMoveCursorArrowsScroll(t *testing.T) {
	t.Parallel()
	// 40 rows, changes at 20 and 30, body 10 (height 12). Opens with the
	// cursor on line 20 and offset 17.
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	v := m.diffLayer()
	if v.curLine != 20 || v.offset != 17 {
		t.Fatalf("open: curLine=%d offset=%d, want 20/17", v.curLine, v.offset)
	}
	u, _ := m.Update(keyMsg("j"))
	v = u.(Model).diffLayer()
	if v.curLine != 21 || v.offset != 17 {
		t.Fatalf("j: curLine=%d offset=%d, want 21/17 (no scroll needed)", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("k"))
	u, _ = u.(Model).Update(keyMsg("k"))
	v = u.(Model).diffLayer()
	if v.curLine != 19 {
		t.Fatalf("k k: curLine=%d, want 19", v.curLine)
	}
	// Arrows move the viewport only: the cursor stays on 19 even off-screen.
	for i := 0; i < 20; i++ {
		u, _ = u.(Model).Update(keyMsg("down"))
	}
	v = u.(Model).diffLayer()
	if v.curLine != 19 || v.offset != 30 {
		t.Fatalf("20×down: curLine=%d offset=%d, want 19/30", v.curLine, v.offset)
	}
	// The next j pulls the cursor back into view minimally (offset = 20).
	u, _ = u.(Model).Update(keyMsg("j"))
	v = u.(Model).diffLayer()
	if v.curLine != 20 || v.offset != 20 {
		t.Fatalf("j from off-screen: curLine=%d offset=%d, want 20/20", v.curLine, v.offset)
	}
}

func TestDiffKeysPageHomeEndMoveCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	u, _ := m.Update(keyMsg("pgdown"))
	v := u.(Model).diffLayer()
	if v.curLine != 30 || v.offset != 27 {
		t.Fatalf("pgdown: curLine=%d offset=%d, want 30/27", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("home"))
	v = u.(Model).diffLayer()
	if v.curLine != 0 || v.offset != 0 {
		t.Fatalf("home: curLine=%d offset=%d, want 0/0", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("end"))
	v = u.(Model).diffLayer()
	if v.curLine != 39 || v.offset != 30 {
		t.Fatalf("end: curLine=%d offset=%d, want 39/30", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("pgup"))
	v = u.(Model).diffLayer()
	if v.curLine != 29 || v.offset != 20 {
		t.Fatalf("pgup: curLine=%d offset=%d, want 29/20", v.curLine, v.offset)
	}
}

func TestDiffKeyNPSeedCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	u, _ := m.Update(keyMsg("n"))
	if v := u.(Model).diffLayer(); v.curLine != 30 {
		t.Fatalf("n: curLine=%d, want 30", v.curLine)
	}
	u, _ = u.(Model).Update(keyMsg("p"))
	if v := u.(Model).diffLayer(); v.curLine != 20 {
		t.Fatalf("p: curLine=%d, want 20", v.curLine)
	}
}

func TestDiffKeyZCyclesAlignment(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(60), nil)
	m.diffLayer().setCursorLine(30, m.diffBodyRows())
	u, _ := m.Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 25 {
		t.Fatalf("z (center): offset=%d, want 25", v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 30 {
		t.Fatalf("z z (top): offset=%d, want 30", v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 21 {
		t.Fatalf("z z z (bottom): offset=%d, want 21", v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 25 {
		t.Fatalf("fourth z wraps to center: offset=%d, want 25", v.offset)
	}
	// Any other key resets the cycle: j then z centers again.
	u, _ = u.(Model).Update(keyMsg("z")) // top
	u, _ = u.(Model).Update(keyMsg("j"))
	u, _ = u.(Model).Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 26 {
		t.Fatalf("j resets the cycle, z centers line 31: offset=%d, want 26", v.offset)
	}
}

func TestDiffToggleKeepsCursorRow(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	m.diffLayer().setCursorLine(31, m.diffBodyRows())
	u, _ := m.Update(keyMsg("f")) // partial
	r, ok := u.(Model).diffLayer().cursorRow()
	if !ok || r.RightNo != 32 {
		t.Fatalf("after f: cursorRow=%+v ok=%v, want RightNo 32", r, ok)
	}
	u, _ = u.(Model).Update(keyMsg("ctrl+w")) // wrap
	r, ok = u.(Model).diffLayer().cursorRow()
	if !ok || r.RightNo != 32 {
		t.Fatalf("after ctrl+w: cursorRow=%+v ok=%v, want RightNo 32", r, ok)
	}
}

func TestDiffHeaderNamesCursorLine(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	m.width = 100
	head := strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "line 21") {
		t.Fatalf("header %q must name the cursor line (line 21)", head)
	}
	rows := cursorRows(5)
	rows[2] = textdiff.Row{Kind: textdiff.Del, Left: "gone", LeftNo: 3}
	for i := 3; i < 5; i++ {
		rows[i].RightNo--
	}
	m = openedDiffModel(12, rows, []int{2})
	m.width = 100
	head = strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "old line 3") {
		t.Fatalf("header %q must say old line 3 on a Del row", head)
	}
}

func TestDiffLeftClickPlacesCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	// Body row 5 (y = 6: header is y 0, body starts at y 1) at offset 17 → display row 22.
	u, _ := m.Update(mouseMsg(10, 6, tea.MouseButtonLeft))
	if v := u.(Model).diffLayer(); v.curLine != 22 {
		t.Fatalf("click y=6: curLine=%d, want 22", v.curLine)
	}
	// A click on the header row (y 0) is inert.
	u, _ = u.(Model).Update(mouseMsg(10, 0, tea.MouseButtonLeft))
	if v := u.(Model).diffLayer(); v.curLine != 22 {
		t.Fatalf("click on the header moved the cursor to %d", v.curLine)
	}
}
