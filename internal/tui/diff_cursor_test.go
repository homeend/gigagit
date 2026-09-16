package tui

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// leadingSGR matches a run of SGR escape sequences at the very start of a
// string — the "carried forward" state ansi.TruncateLeft prepends so a cut
// fragment renders correctly standalone.
var leadingSGR = regexp.MustCompile(`^(?:\x1b\[[0-9;]*m)+`)

// trimLeadingSGR strips that carried-forward prefix so two cut fragments can
// be compared for their actual content past the cut point.
func trimLeadingSGR(s string) string {
	return leadingSGR.ReplaceAllString(s, "")
}

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

// TestReanchorCursorFallsBackWhenRowHiddenByFold: the source row the cursor
// was on got folded away by the partial-mode context window (not just moved),
// so reanchorCursor's search loop never finds a match. The not-found fallback
// must snap curLine off whatever fold it is (stalely) sitting on and land it
// on a real row, still in range.
func TestReanchorCursorFallsBackWhenRowHiddenByFold(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40, 20), []int{20})
	v.partial = true
	v.rebuild()
	if v.lines[0].Fold == 0 {
		t.Fatal("fixture: expected line 0 to be a fold in partial mode")
	}
	v.curLine = 0 // stale index left sitting on the fold
	v.reanchorCursor(1, 1)
	if v.curLine < 0 || v.curLine >= len(v.lines) {
		t.Fatalf("curLine=%d out of range [0,%d)", v.curLine, len(v.lines))
	}
	if v.lines[v.curLine].Fold > 0 {
		t.Fatalf("not-found fallback must snap off the fold, got line %d (Fold=%d)", v.curLine, v.lines[v.curLine].Fold)
	}
}

// TestCursorRowAndDispRangeOnEmptyView: an empty diff (no rows) must report
// "no cursor" rather than indexing an empty slice.
func TestCursorRowAndDispRangeOnEmptyView(t *testing.T) {
	t.Parallel()
	v := diffViewWith(nil, nil)
	if _, ok := v.cursorRow(); ok {
		t.Fatal("cursorRow on an empty view must return false")
	}
	if s, e := v.cursorDispRange(); s != 0 || e != 0 {
		t.Fatalf("cursorDispRange on an empty view = (%d,%d), want (0,0)", s, e)
	}
}

// TestCursorMoversNoopOnEmptyView: every cursor mover must tolerate an empty
// view (no lines, no display rows) without panicking, leaving curLine at its
// zero value.
func TestCursorMoversNoopOnEmptyView(t *testing.T) {
	t.Parallel()
	v := diffViewWith(nil, nil)
	body := 10
	v.moveCursor(1, body)
	v.setCursorLine(3, body)
	v.setCursorDisp(0, body)
	v.alignCursor(alignTop, body)
	v.pageCursor(1, body)
	if v.curLine != 0 {
		t.Fatalf("curLine=%d after movers on an empty view, want 0", v.curLine)
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

func TestDiffKeysAltArrowsAliasJK(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	if v := u.(Model).diffLayer(); v.curLine != 21 || v.offset != 17 {
		t.Fatalf("alt+down: curLine=%d offset=%d, want 21/17", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	u, _ = u.(Model).Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	if v := u.(Model).diffLayer(); v.curLine != 19 {
		t.Fatalf("alt+up ×2: curLine=%d, want 19", v.curLine)
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
	// With the . menu open over the diff the click belongs to the menu — it
	// must not move the cursor hidden behind it.
	mm := u.(Model).openActionMenu()
	if mm.actionMenu == nil {
		t.Fatal("setup: . must open the action menu over the diff")
	}
	u2, _ := mm.Update(mouseMsg(10, 3, tea.MouseButtonLeft))
	if v := u2.(Model).diffLayer(); v.curLine != 22 {
		t.Fatalf("click while the . menu is open moved the cursor to %d, want 22", v.curLine)
	}
}

// NOTE: no t.Parallel() here or in TestCursorMarkerSkipsFoldRow /
// TestHistoryPaneHasNoCursorMarker / TestCursorMarkerHotRowWinsOverCursorBackground
// — they assert on the actual rendered background/foreground codes, so they
// force the color profile like diff_render_test.go's
// TestEmphasisActuallyChangesOutput does. lipgloss.SetColorProfile is
// process-global; a parallel sibling's deferred reset could flip the profile
// mid-render for another goroutine.
func TestCursorMarkerRowPaintsOnlyCursorRows(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	m := openedDiffModel(12, cursorRows(40), nil)
	m.width = 80
	v := m.diffLayer()
	v.setCursorLine(5, m.diffBodyRows())
	s, e := v.cursorDispRange()
	marked := m.diffPaneLines(v, 80, 10, s, e, "row")
	plain := m.diffPaneLines(v, 80, 10, 0, 0, "row")
	if len(marked) != len(plain) {
		t.Fatal("row counts differ")
	}
	for i := range marked {
		isCur := i+v.offset >= s && i+v.offset < e
		if isCur && marked[i] == plain[i] {
			t.Fatalf("row %d is the cursor row but renders unchanged", i)
		}
		if !isCur && marked[i] != plain[i] {
			t.Fatalf("row %d is not the cursor row but renders differently", i)
		}
	}
	// "off" is byte-identical to no marker; "number" changes the row but not the way "row" does.
	if off := m.diffPaneLines(v, 80, 10, s, e, "off"); !reflect.DeepEqual(off, plain) {
		t.Fatal("off must render exactly like no marker")
	}
	num := m.diffPaneLines(v, 80, 10, s, e, "number")
	if num[s-v.offset] == plain[s-v.offset] || num[s-v.offset] == marked[s-v.offset] {
		t.Fatal("number style must differ from both plain and row")
	}
	// Localize the "number" difference: split the cursor row at the pane
	// separator and check each pane on its own. The style only recolors the
	// gutter (the first gutterWidth+1 display columns) — the body text after
	// it must be byte-identical to the plain render.
	gut := gutterWidth(v.full)
	row := s - v.offset
	numPanes := strings.SplitN(num[row], "│", 2)
	plainPanes := strings.SplitN(plain[row], "│", 2)
	if len(numPanes) != 2 || len(plainPanes) != 2 {
		t.Fatalf("cursor row must have exactly one pane separator: %q", num[row])
	}
	// The cursor sits on ONE pane (spec §4.7; the default is the new/right
	// one), so only that pane's gutter is recoloured — the other must be
	// byte-identical to the plain render.
	curPane := 1
	if v.onOld {
		curPane = 0
	}
	for i := range numPanes {
		if i != curPane {
			if numPanes[i] != plainPanes[i] {
				t.Fatalf("pane %d does not hold the cursor: it must render exactly like plain\n got  %q\n want %q", i, numPanes[i], plainPanes[i])
			}
			continue
		}
		numGut, plainGut := ansi.Truncate(numPanes[i], gut+1, ""), ansi.Truncate(plainPanes[i], gut+1, "")
		if numGut == plainGut {
			t.Fatalf("pane %d: number style gutter unchanged from plain: %q", i, numGut)
		}
		// ansi.TruncateLeft replays the SGR state active at the cut point as a
		// leading escape prefix so the fragment renders correctly standalone —
		// that prefix legitimately differs (it carries the gutter's own color),
		// so trim it before comparing; what is left must be byte-identical.
		numBody := trimLeadingSGR(ansi.TruncateLeft(numPanes[i], gut+1, ""))
		plainBody := trimLeadingSGR(ansi.TruncateLeft(plainPanes[i], gut+1, ""))
		if numBody != plainBody {
			t.Fatalf("pane %d: number style body must be byte-identical to plain\n got  %q\n want %q", i, numBody, plainBody)
		}
	}
}

func TestCursorMarkerSkipsFoldRow(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	m := openedDiffModel(12, cursorRows(40, 20), []int{20})
	m.width = 80
	v := m.diffLayer()
	v.partial = true
	v.rebuild()
	foldRow := -1
	for i, d := range v.disp {
		if d.fold > 0 {
			foldRow = i
			break
		}
	}
	v.offset = 0
	// Force the "cursor" range onto the fold row: the renderer must not paint it.
	got := m.diffPaneLines(v, 80, 10, foldRow, foldRow+1, "row")
	plain := m.diffPaneLines(v, 80, 10, 0, 0, "row")
	if got[foldRow] != plain[foldRow] {
		t.Fatal("a fold separator must never carry the cursor marker")
	}
}

// TestCursorMarkerHotRowStepsBrighter: when the cursor lands on a Changed
// row, the row's add/del cells keep their meaning but step one shade
// brighter (52 → 88, 22 → 28) instead of taking the grey band (237), so the
// cursor stays visible on exactly the rows a reviewer stops on. A Same
// neighbour row still carries 237.
func TestCursorMarkerHotRowStepsBrighter(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	m := openedDiffModel(12, cursorRows(40, 20), []int{20})
	m.width = 80
	v := m.diffLayer()

	v.setCursorLine(20, m.diffBodyRows()) // the Changed row
	s, e := v.cursorDispRange()
	// The cursor sits on ONE side (spec §4.7), so only that cell steps
	// brighter; the other keeps its own plain shade. Both halves of the rule
	// are asserted by flipping the side below.
	hot := m.diffPaneLines(v, 80, 10, s, e, "row")
	hotRow := hot[s-v.offset]
	if !strings.Contains(hotRow, "48;5;28") || strings.Contains(hotRow, "48;5;22") {
		t.Fatalf("the new-side cursor cell must wear the brighter add shade (28, not 22): %q", hotRow)
	}
	if !strings.Contains(hotRow, "48;5;52") || strings.Contains(hotRow, "48;5;88") {
		t.Fatalf("the old-side cell is not the cursor's: it must keep the plain del shade (52, not 88): %q", hotRow)
	}
	if strings.Contains(hotRow, "48;5;237") {
		t.Fatalf("hot cursor row must not carry the grey band (237): %q", hotRow)
	}
	v.onOld = true
	oldRow := m.diffPaneLines(v, 80, 10, s, e, "row")[s-v.offset]
	if !strings.Contains(oldRow, "48;5;88") || strings.Contains(oldRow, "48;5;52") {
		t.Fatalf("the old-side cursor cell must wear the brighter del shade (88, not 52): %q", oldRow)
	}
	if !strings.Contains(oldRow, "48;5;22") || strings.Contains(oldRow, "48;5;28") {
		t.Fatalf("the new-side cell is not the cursor's: it must keep the plain add shade (22, not 28): %q", oldRow)
	}
	v.onOld = false
	off := m.diffPaneLines(v, 80, 10, 0, 0, "row")
	offRow := off[s-v.offset]
	if !strings.Contains(offRow, "48;5;52") || !strings.Contains(offRow, "48;5;22") {
		t.Fatalf("the same row without the cursor must keep 52/22: %q", offRow)
	}
	v.setCursorLine(21, m.diffBodyRows()) // a Same neighbour
	s, e = v.cursorDispRange()
	same := m.diffPaneLines(v, 80, 10, s, e, "row")
	if !strings.Contains(same[s-v.offset], "48;5;237") {
		t.Fatalf("Same cursor row must carry the grey band (237): %q", same[s-v.offset])
	}
}

func TestCursorStyleSessionOverrideAndCycle(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	if m.cursorStyle() != "row" {
		t.Fatalf("default style = %q, want row", m.cursorStyle())
	}
	m.cfg.UI.DiffCursor = "number"
	if m.cursorStyle() != "number" {
		t.Fatalf("config number: style = %q", m.cursorStyle())
	}
	m.diffCursor = "off"
	if m.cursorStyle() != "off" {
		t.Fatalf("session off must win: %q", m.cursorStyle())
	}
	if nextCursorStyle("row") != "number" || nextCursorStyle("number") != "off" || nextCursorStyle("off") != "row" {
		t.Fatal("cycle must be row → number → off → row")
	}
}

func TestHistoryPaneHasNoCursorMarker(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	m := openedDiffModel(12, cursorRows(40), nil)
	m.width = 80
	v := m.diffLayer()
	v.setCursorLine(5, m.diffBodyRows())
	if got := m.diffPaneLines(v, 80, 10, 0, 0, m.cursorStyle()); len(got) != 10 {
		t.Fatalf("rows = %d", len(got))
	}
	// The full-screen render marks the cursor; strip the marker style and it must
	// equal the unmarked render (proves the marker is the only difference).
	full := strings.Split(m.renderDiffView(), "\n")[1:11]
	plain := m.diffPaneLines(v, 80, 10, 0, 0, "off")
	diffRows := 0
	for i := range plain {
		if full[i] != plain[i] {
			diffRows++
		}
	}
	if diffRows != 1 {
		t.Fatalf("full-screen render differs from the unmarked pane on %d rows, want exactly 1 (the cursor)", diffRows)
	}
}

func TestDiffEditLineRule(t *testing.T) {
	t.Parallel()
	rows := cursorRows(6)
	rows[2] = textdiff.Row{Kind: textdiff.Del, Left: "gone", LeftNo: 3}
	for i := 3; i < 6; i++ {
		rows[i].RightNo-- // new side: 1,2,_,3,4,5
	}
	v := diffViewWith(rows, []int{2})
	v.setCursorLine(1, 10)
	if got := v.editLine(); got != 2 {
		t.Fatalf("Same row: editLine=%d, want 2", got)
	}
	v.setCursorLine(2, 10) // the Del row: next row with a new-side number → 3
	if got := v.editLine(); got != 3 {
		t.Fatalf("Del row: editLine=%d, want 3", got)
	}
	rows2 := cursorRows(3)
	rows2[2] = textdiff.Row{Kind: textdiff.Del, Left: "tail", LeftNo: 3}
	v2 := diffViewWith(rows2, []int{2})
	v2.setCursorLine(2, 10) // trailing deletion: no following new line → the last one (2)
	if got := v2.editLine(); got != 2 {
		t.Fatalf("trailing Del: editLine=%d, want 2", got)
	}
}

func TestDiffEditRowGating(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(10), nil)
	if _, ok := m.diffEditRow(); !ok {
		t.Fatal("working-tree diff must offer the edit row")
	}
	m.diffLayer().compare = true
	if _, ok := m.diffEditRow(); ok {
		t.Fatal("a compare view has no single file: no edit row")
	}
	m.diffLayer().compare = false
	m.diffLayer().rev = "abc123"
	r, ok := m.diffEditRow()
	if !ok || !strings.Contains(r.label, "line 1") {
		t.Fatalf("commit diff row = %+v ok=%v, want an 'at line 1' row", r, ok)
	}
	m.diffLayer().rev = ""

	m.diffLayer().loading = true
	if _, ok := m.diffEditRow(); ok {
		t.Fatal("a loading view has no file to edit yet: no edit row")
	}
	m.diffLayer().loading = false

	m.diffLayer().err = errors.New("boom")
	if _, ok := m.diffEditRow(); ok {
		t.Fatal("an errored view: no edit row")
	}
	m.diffLayer().err = nil

	m.diffLayer().binary = true
	if _, ok := m.diffEditRow(); ok {
		t.Fatal("a binary file: no edit row")
	}
	m.diffLayer().binary = false

	m.diffLayer().tooLarge = true
	if _, ok := m.diffEditRow(); ok {
		t.Fatal("a too-large file: no edit row")
	}
	m.diffLayer().tooLarge = false
}

func TestDiffEKeyWorkingTreeUsesLiveEditor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(10), nil)
	m.currentWorktree = "/wt"
	m.diffLayer().setCursorLine(4, m.diffBodyRows())
	_, cmd := m.Update(keyMsg("e"))
	if cmd == nil {
		t.Fatal("e on a working-tree diff must return an editor command")
	}
}

func TestDiffMenuOffersCursorRows(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	for _, want := range []string{"diff-edit-at-line", "diff-cursor-style", "diff-align-top", "diff-align-center", "diff-align-bottom"} {
		if !ids[want] {
			t.Errorf("diff . menu lacks %s (have %v)", want, ids)
		}
	}
}

func TestDiffAlignMenuRowFeedsZCycle(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(60), nil)
	m.diffLayer().setCursorLine(30, m.diffBodyRows())
	var top actionRow
	found := false
	for _, r := range availableActions(m) {
		if r.id == "diff-align-top" {
			top, found = r, true
		}
	}
	if !found {
		t.Fatal("diff-align-top row not offered")
	}
	nm, _ := top.run(m)
	m = nm.(Model)
	if v := m.diffLayer(); v.offset != 30 {
		t.Fatalf("Align cursor line: top: offset=%d, want 30", v.offset)
	}
	u, _ := m.Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 21 {
		t.Fatalf("z after the menu's top row: offset=%d, want 21 (bottom)", v.offset)
	}
}

func TestDiffHintAdvertisesCursorKeys(t *testing.T) {
	t.Parallel()
	h := diffHintFor(longScroll)
	for _, k := range []string{"[↑↓/jk]", "[spc]", "[alt↔]", "[e]"} {
		if !strings.Contains(h, k) {
			t.Errorf("hint %q lacks %s", h, k)
		}
	}
}

// TestDiffPartialToggleKeepsFreeScrolledViewport: arrows move the viewport
// only, so after a free scroll the cursor is left behind off-screen while cur
// tracks the new offset. f must re-anchor the SAME change (what the user is
// looking at) and must not drag the viewport back to the stale cursor line —
// the cursor is re-found by its source numbers, but the view stays put.
func TestDiffPartialToggleKeepsFreeScrolledViewport(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	body := m.diffBodyRows() // 10
	for i := 0; i < 13; i++ {
		u, _ := m.Update(keyMsg("down"))
		m = u.(Model)
	}
	if v := m.diffLayer(); v.offset != 30 || v.curLine != 20 || v.cur != 1 {
		t.Fatalf("setup, 13×down: offset=%d curLine=%d cur=%d, want 30/20/1", v.offset, v.curLine, v.cur)
	}
	// Reference: where focusBlock(1) alone lands the viewport in partial mode,
	// derived by the same code with no cursor logic in the way.
	ref := diffViewWith(cursorRows(40, 20, 30), []int{20, 30})
	ref.partial = true
	ref.rebuild()
	ref.focusBlock(1, body)

	u, _ := m.Update(keyMsg("f"))
	v := u.(Model).diffLayer()
	if v.offset != ref.offset {
		t.Fatalf("f after a free scroll: offset=%d, want %d (where focusBlock(1) puts it)", v.offset, ref.offset)
	}
	if v.cur != 1 {
		t.Fatalf("f must keep the focused change: cur=%d, want 1", v.cur)
	}
	if r, ok := v.cursorRow(); !ok || r.RightNo != 21 {
		t.Fatalf("f must re-anchor the cursor row: %+v %v, want RightNo 21", r, ok)
	}
}

// Sibling: when the cursor WAS on screen before the toggle it must still be on
// screen after (the minimal scroll still runs in that case).
func TestDiffPartialToggleKeepsAVisibleCursorVisible(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	body := m.diffBodyRows()
	v0 := m.diffLayer()
	if s, _ := v0.cursorDispRange(); s < v0.offset || s >= v0.offset+body {
		t.Fatalf("setup: cursor row %d not visible in [%d,%d)", s, v0.offset, v0.offset+body)
	}
	u, _ := m.Update(keyMsg("f"))
	v := u.(Model).diffLayer()
	s, _ := v.cursorDispRange()
	if s < v.offset || s >= v.offset+body {
		t.Fatalf("f with a visible cursor: row %d fell out of [%d,%d)", s, v.offset, v.offset+body)
	}
	if r, ok := v.cursorRow(); !ok || r.RightNo != 21 {
		t.Fatalf("cursorRow after f = %+v %v, want RightNo 21", r, ok)
	}
}

// TestDiffPgDownMovesOneBodyOfDisplayRowsWhenWrapped: in wrap mode a logical
// line owns several display rows, so a page key that stepped the cursor by
// `body` LINES would drag the viewport a further page (ensureCursorVisible
// follows the cursor) — a pgdown scrolling ~two screens. The page keys move
// the cursor by one body of DISPLAY rows, matching the scroll they pair with.
func TestDiffPgDownMovesOneBodyOfDisplayRowsWhenWrapped(t *testing.T) {
	t.Parallel()
	rows := cursorRows(30)
	long := strings.Repeat("ab ", 8) // 24 cols: two display rows in a 16-col pane
	for i := range rows {
		rows[i].Left, rows[i].Right = long, long
	}
	m := openedDiffModel(12, rows, nil)
	body := m.diffBodyRows() // 10
	v0 := m.diffLayer()
	v0.long = longWrap
	v0.relayout(40)
	if len(v0.disp) != 2*len(v0.lines) {
		t.Fatalf("fixture: %d display rows for %d lines, want each line to wrap to exactly 2",
			len(v0.disp), len(v0.lines))
	}
	if v0.offset != 0 || v0.curLine != 0 {
		t.Fatalf("setup: offset=%d curLine=%d, want 0/0", v0.offset, v0.curLine)
	}

	u, _ := m.Update(keyMsg("pgdown"))
	v := u.(Model).diffLayer()
	if v.offset != body {
		t.Fatalf("pgdown in wrap mode: offset=%d, want %d (exactly one body)", v.offset, body)
	}
	if start, _ := v.cursorDispRange(); start != body {
		t.Fatalf("pgdown in wrap mode: cursor display row=%d, want %d (exactly one body)", start, body)
	}
}

// TestShelfCompareTwoEntriesIsACompareView: a shelf ↔ shelf diff has NO
// working-tree side, and its title is "a.txt ↔ b.txt" — if the view were not
// marked compare, `e` would pass diffEditRow's gate and, with rev == "", ask
// the editor to open <worktree>/a.txt ↔ b.txt, creating a stray file.
func TestShelfCompareTwoEntriesIsACompareView(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m.height = 12
	a := model.ShelfEntry{ID: "sh-a", SHA: "aaaaaaaabbbb", Origin: model.FileAddress{Path: "a.txt"}}
	b := model.ShelfEntry{ID: "sh-b", SHA: "ccccccccdddd", Origin: model.FileAddress{Path: "b.txt"}}
	nm, _ := m.openShelfCompareTwoEntries(a, b)
	v := nm.diffLayer()
	if v == nil {
		t.Fatal("openShelfCompareTwoEntries must push a diff layer")
	}
	if !v.compare {
		t.Fatalf("shelf ↔ shelf view is two-sided: compare=%v, want true (title %q)", v.compare, v.title)
	}
	v.loading = false // the gate short-circuits on loading; isolate the compare check
	if _, ok := nm.diffEditRow(); ok {
		t.Fatal("a shelf ↔ shelf compare must not offer the edit row")
	}
	// The loader builds the same shape; diffMsg ORs the opener's flag back in,
	// but the literal must be right on its own.
	lv := m.newShelfCompareTwoView("a.txt ↔ b.txt", "ctx", false)
	if !lv.compare || lv.rev != "" {
		t.Fatalf("loader-side view = compare:%v rev:%q, want true/\"\"", lv.compare, lv.rev)
	}
}

// With [ui] diff_cursor = "number" only the GUTTER carries the cursor, so the
// row body is free to keep the agent's attention band — and the row the user is
// sitting on is precisely the one they are most likely to be looking at. (No
// t.Parallel(): asserts on rendered SGR codes, see the note above
// TestCursorMarkerRowPaintsOnlyCursorRows.)
func TestCursorNumberModeKeepsTheAttentionBand(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	m := openedDiffModel(12, cursorRows(40), nil)
	m.width = 80
	v := m.diffLayer()
	v.noteAddr = model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}
	v.setCursorLine(5, m.diffBodyRows())
	s, e := v.cursorDispRange()
	row := s - v.offset

	plain := m.diffPaneLines(v, 80, 10, s, e, "number")
	m.attention = map[attentionKey][]steerMark{
		{path: "a.txt", state: "unstaged"}: {{side: "new", start: 1, end: 40, tone: "warn"}},
	}
	banded := m.diffPaneLines(v, 80, 10, s, e, "number")
	if banded[row] == plain[row] {
		t.Fatal("number-cursor mode dropped the attention band on the cursor row")
	}
	// Sanity: a non-cursor row in range was banded either way.
	if other := row + 1; other < len(banded) && banded[other] == plain[other] {
		t.Fatal("setup: a non-cursor row inside the band must render banded")
	}
	// …and the cursor gutter style must survive ON TOP of the band.
	noCursor := m.diffPaneLines(v, 80, 10, 0, 0, "number")
	if banded[row] == noCursor[row] {
		t.Fatal("the number-cursor gutter style was lost when the band was applied")
	}
}
