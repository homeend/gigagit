package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/textdiff"
)

// textRows is n Same rows whose both sides read the given lines (cycled).
func textRows(lines ...string) []textdiff.Row {
	out := make([]textdiff.Row, len(lines))
	for i, l := range lines {
		out[i] = textdiff.Row{Kind: textdiff.Same, Left: l, Right: l, LeftNo: i + 1, RightNo: i + 1}
	}
	return out
}

// v / l l / space / j / end / enter copies the cursor side's text from the
// start column to the end column across the lines, joined with newlines.
func TestDiffCharSelCopiesAcrossLines(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("hello world", "second line", "third"), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v")
	if !v.cs.on || v.cs.cur != (pos{0, 0}) {
		t.Fatalf("v: %+v", v.cs)
	}
	m = feedDiff(m, "l", "l", "space", "j", "end")
	if v.curLine != 1 {
		t.Fatalf("the line cursor must follow the character cursor's row: curLine = %d", v.curLine)
	}
	u, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, u.(Model), cmd)
	if got != "llo world\nsecond line" || v.cs.on {
		t.Fatalf("copied %q (on=%v)", got, v.cs.on)
	}
}

// Review Focus 2: a tab paints its expanded cells and copies as a tab.
func TestDiffCharSelTabsPaintExpandedAndCopyRaw(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("a\tb", "x"), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "space", "l", "l") // a, the tab, b
	sp := v.charSpansOn(0)
	if len(sp) != 2 || sp[0].start != 0 || sp[0].end != 5 || sp[1].start != 4 || sp[1].end != 5 {
		t.Fatalf("spans = %+v, want the stripe over a + 3 tab cells + b (0..5) and the cursor on b (4..5)", sp)
	}
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "a\tb" {
		t.Fatalf("copied %q, want the raw tab", got)
	}
	if dispCol("\tx", 1) != 4 || dispCol("ab", 2) != 2 || dispCol("a\tb", 2) != 4 {
		t.Fatalf("dispCol: %d %d %d", dispCol("\tx", 1), dispCol("ab", 2), dispCol("a\tb", 2))
	}
}

// The mode is modal in the diff: alt+←/→ (the side), n/p, / and S are
// inert; v clears a live line selection (one selection kind at a time).
func TestDiffCharSelIsModalAndClearsTheLineSelection(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("one", "two", "three"), nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	m = feedDiff(m, "space") // a line selection
	if !v.lsel.on {
		t.Fatal("space must start a line selection")
	}
	m = feedDiff(m, "v")
	if v.lsel.on || !v.cs.on {
		t.Fatalf("v must clear the line selection and enter the mode: lsel=%+v cs=%+v", v.lsel, v.cs)
	}
	m = feedDiff(m, "alt+left", "n", "/", "S")
	if v.onOld || !v.cs.on || v.search.typing || v.stk != nil {
		t.Fatalf("keys leaked through the mode: onOld=%v on=%v typing=%v stacked=%v", v.onOld, v.cs.on, v.search.typing, v.stk != nil)
	}
	if hint := strings.Join(strings.Fields(m.View()), " "); !strings.Contains(hint, "[space] start") {
		t.Fatalf("the footer must show the mode's keys:\n%s", m.View())
	}
}

// The cursor side's absent cell is dead: on an Add row the old side has no
// text, so a selection on the old side steps over it and copies nothing of it.
func TestDiffCharSelSkipsAbsentCellsOnTheCursorSide(t *testing.T) {
	t.Parallel()
	rows := []textdiff.Row{
		{Kind: textdiff.Same, Left: "keep", Right: "keep", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Add, Right: "added", RightNo: 2},
		{Kind: textdiff.Same, Left: "tail", Right: "tail", LeftNo: 2, RightNo: 3},
	}
	m := openedDiffModel(12, rows, nil)
	v := m.diffLayer()
	v.setCursorLine(0, m.diffBodyRows())
	m = feedDiff(m, "alt+left") // the old side
	var got string
	m = captureClip(m, &got)
	m = feedDiff(m, "v", "space", "j", "end")
	if v.cs.cur.row != 2 {
		t.Fatalf("j must step over the absent old cell: row %d", v.cs.cur.row)
	}
	u, cmd := m.Update(keyMsg("enter"))
	drainCmds(t, u.(Model), cmd)
	if got != "keep\ntail" {
		t.Fatalf("copied %q", got)
	}
}

// Review Focus 5: a rebuild (f, the partial toggle) leaves the mode.
func TestDiffCharSelLeavesOnRebuild(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, textRows("one", "two", "three"), nil)
	v := m.diffLayer()
	m = feedDiff(m, "v", "space", "l")
	v.rebuild()
	if v.cs.on {
		t.Fatal("a rebuild must leave the mode")
	}
}

// Nothing to select: a loading view consumes v and says so.
func TestDiffCharSelOnALoadingViewSaysSo(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, nil, nil)
	v := m.diffLayer()
	v.loading = true
	m = feedDiff(m, "v")
	if v.cs.on || m.diffNotice == "" {
		t.Fatalf("on=%v notice=%q", v.cs.on, m.diffNotice)
	}
}
