package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/textdiff"
)

// NOTE: TestDiffSelectionPaintsOnlyTheCursorSide and
// TestDiffSelectionOnAChangedRowStillPaints call lipgloss.SetColorProfile
// (process-global) and therefore do NOT call t.Parallel().

// selRow finds an action row by id in the . menu's context copy rows.
func selRow(t *testing.T, m Model, id string) actionRow {
	t.Helper()
	r, ok := rowByID(m.contextCopyRows(), id)
	if !ok {
		var ids []string
		for _, x := range m.contextCopyRows() {
			ids = append(ids, x.id)
		}
		t.Fatalf("row %q not offered; rows = %v", id, ids)
	}
	return r
}

// space / move / space / move / enter copies exactly the FROZEN range — the
// second space fixes the end and the cursor is free afterwards. The payload is
// read off the action row the enter key runs, so the test sees the real text.
func TestDiffSelectionCopiesTheFrozenRange(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil) // 40 Same rows: Left "l", Right "r"
	m.diffLayer().setCursorLine(5, m.diffBodyRows())

	m = feedDiff(m, "space", "j", "j", "space", "j", "j") // range 5..7, cursor now 9
	v := m.diffLayer()
	if !v.lsel.on || !v.lsel.fixed {
		t.Fatalf("after two spaces the range must be frozen: %+v", v.lsel)
	}
	if v.curLine != 9 {
		t.Fatalf("the cursor must be free after the freeze, curLine = %d want 9", v.curLine)
	}
	row := selRow(t, m, "copy-selected-lines")
	if row.label != "Copy selected lines (3)" {
		t.Fatalf("label = %q, want `Copy selected lines (3)`", row.label)
	}
	if row.copyText != "r\nr\nr" {
		t.Fatalf("copyText = %q, want the three new-side lines joined with \\n and no trailing newline", row.copyText)
	}

	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if cmd == nil {
		t.Fatal("enter with a selection on must issue the clipboard command")
	}
	if m.diffLayer().lsel.on {
		t.Fatal("enter must clear the selection after copying")
	}
}

// A ONE-space selection copies anchor..cursor, the way tmux does.
func TestDiffOneSpaceSelectionFollowsTheCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	m.diffLayer().setCursorLine(2, m.diffBodyRows())
	m = feedDiff(m, "space", "j")
	if got := selRow(t, m, "copy-selected-lines").copyText; got != "r\nr" {
		t.Fatalf("copyText = %q, want two lines (anchor..cursor)", got)
	}
}

// The range takes the CURSOR SIDE's text and skips a cell that side does not
// have: on the new side a Del row contributes nothing, on the old side an Add
// row does not.
func TestDiffSelectionSkipsAbsentCells(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll) // Same, Add, Del
	v := m.diffLayer()
	v.curLine = 0
	m = feedDiff(m, "space")
	v.curLine = 2 // loose range 0..2

	if got := selRow(t, m, "copy-selected-lines").copyText; got != "same-left\nadded-right" {
		t.Fatalf("new side copyText = %q, want the Same and Add rows only", got)
	}
	if lbl := selRow(t, m, "copy-selected-lines").label; lbl != "Copy selected lines (2)" {
		t.Fatalf("label = %q, want a count of 2 (the Del row has no new cell)", lbl)
	}

	v.onOld = true
	if got := selRow(t, m, "copy-selected-lines").copyText; got != "same-left\ngone-left" {
		t.Fatalf("old side copyText = %q, want the Same and Del rows only", got)
	}
}

// A range that crosses a collapsed FOLD copies only the visible lines (the
// user's ruling): a fold entry is a separator, not a line.
func TestDiffSelectionSkipsFolds(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 4, 24), []int{4, 24})
	v := m.diffLayer()
	v.partial = true
	v.rebuild() // partial mode collapses the unchanged runs into fold entries

	folds := 0
	for _, ln := range v.lines {
		if ln.Fold > 0 {
			folds++
		}
	}
	if folds == 0 {
		t.Fatal("the fixture must produce fold entries in partial mode")
	}
	// Line 0 of a partial stream is usually the LEADING fold; snap off it so the
	// fixture does not encode an assumption about the stream's shape.
	v.curLine = v.snapOffFold(0)
	v.lsel.start(0)
	v.lsel.mark(len(v.lines) - 1) // the whole stream

	got := v.selectedLines()
	if len(got) != len(v.lines)-folds {
		t.Fatalf("copied %d lines over a stream of %d with %d folds; folds must be skipped", len(got), len(v.lines), folds)
	}
	for _, s := range got {
		if s != "r" && s != "y" {
			t.Fatalf("an unexpected line %q leaked into the copy — only the new side's text belongs there", s)
		}
	}
}

// A range with NOTHING copyable on this side is a no-op with a notice, not a
// clipboard command carrying an empty string.
func TestDiffSelectionWithNothingOnThisSideNotices(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	v := m.diffLayer()
	v.onOld = true
	v.curLine = 1 // the Add row: the old side is a gap
	v.lsel.start(1)
	v.lsel.mark(1)

	if _, ok := rowByID(m.contextCopyRows(), "copy-selected-lines"); ok {
		t.Fatal("an empty range must offer no Copy selected lines row")
	}
	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)
	if cmd != nil {
		t.Fatal("enter over an empty range must not touch the clipboard")
	}
	if !strings.Contains(m.diffNotice, "nothing to copy") {
		t.Fatalf("diffNotice = %q, want the nothing-to-copy notice", m.diffNotice)
	}
	if m.diffLayer().lsel.on {
		t.Fatal("enter must clear the selection either way")
	}
}

// esc clears the selection and nothing else; the SECOND esc closes the view.
func TestDiffEscClearsSelectionBeforeClosing(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	m = feedDiff(m, "space")
	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.diffLayer() == nil {
		t.Fatal("the first esc must clear the selection, not close the view")
	}
	if m.diffLayer().lsel.on {
		t.Fatal("the first esc must clear the selection")
	}
	u, _ = m.Update(keyMsg("esc"))
	if u.(Model).diffLayer() != nil {
		t.Fatal("the second esc must close the view")
	}
}

// f (the partial toggle) rebuilds the line stream, so the indexes the range
// holds mean nothing any more: the selection goes.
func TestDiffRebuildClearsSelection(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 4, 24), []int{4, 24})
	m = feedDiff(m, "space")
	if !m.diffLayer().lsel.on {
		t.Fatal("space must start a selection")
	}
	m = feedDiff(m, "f")
	if m.diffLayer().lsel.on {
		t.Fatal("the partial toggle rebuilds the stream: the selection must be gone")
	}
	m = feedDiff(m, "space")
	m = feedDiff(m, "ctrl+w")
	if m.diffLayer().lsel.on {
		t.Fatal("ctrl+w relayouts and re-anchors the cursor: the selection must be gone")
	}
}

// The side stays locked once a range is live, driven through the real keys.
func TestDiffSelectionLockedToSide(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	m = feedDiff(m, "space", "alt+left")
	if m.diffLayer().onOld {
		t.Fatal("alt+left must not flip the side while a selection is on")
	}
	if !strings.Contains(m.diffNotice, "locked") {
		t.Fatalf("diffNotice = %q, want the lock notice", m.diffNotice)
	}
	m = feedDiff(m, "esc", "alt+left")
	if !m.diffLayer().onOld {
		t.Fatal("with the selection cleared alt+left must flip the side again")
	}
}

// The stripe paints the CURSOR side's cell only, and never the gutter.
func TestDiffSelectionPaintsOnlyTheCursorSide(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	for _, lm := range []longMode{longScroll, longWrap, longTruncate} {
		m := sideModel(lm)
		v := m.diffLayer()
		v.curLine = 0
		v.lsel.start(0)
		v.lsel.mark(0)

		// The stripe's OPENING sequence, derived at runtime: sgrParams over the
		// whole render would also fold in the trailing reset, which no painted
		// cell carries before its text (the hazard diff_side_test.go records).
		stripe := sgrBefore(st().selectionStyle(lipgloss.NewStyle()).Render("x"), "x")
		row := diffBodyRow(t, m, 0)
		if got := sgrBefore(row, "same-left"); subsetOf(stripe, got) {
			t.Errorf("mode %d: the LEFT cell must not be striped while the cursor is on the right: %q", lm, row)
		}
		right := row[strings.Index(row, "│"):]
		if got := sgrBefore(right, "same-left"); !subsetOf(stripe, got) {
			t.Errorf("mode %d: the RIGHT cell must be striped, params %v: %q", lm, got, right)
		}

		// The Del row is NOT in the range and must stay unpainted.
		other := diffBodyRow(t, m, 2)
		if got := sgrBefore(other, "gone-left"); subsetOf(stripe, got) {
			t.Errorf("mode %d: a row outside the range must not be striped: %q", lm, other)
		}
	}
}

// The footer swaps to the selection variant while a range is live, so every key
// that matters is advertised and the user is never trapped.
func TestDiffSelectionFooterVariant(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	// Render WIDE: view.go truncates the hint to the terminal width, and both
	// variants are longer than the 80-column default overlayDims falls back to.
	m.width = 160
	base := lastLine(m.renderDiffView())
	if !strings.Contains(base, "[spc] mark") || !strings.Contains(base, "[alt↔] side") {
		t.Fatalf("the base footer must advertise the new keys: %q", base)
	}
	m = feedDiff(m, "space")
	sel := lastLine(m.renderDiffView())
	for _, want := range []string{"[space] mark end", "[enter] copy", "[esc] unmark", "[alt↔] side (locked)"} {
		if !strings.Contains(sel, want) {
			t.Errorf("the selection footer lacks %q: %q", want, sel)
		}
	}
	if lipgloss.Width(diffSelectHint()) > 140 {
		t.Errorf("the selection hint is %d columns, the budget is 140: %q", lipgloss.Width(diffSelectHint()), diffSelectHint())
	}
}

// A Changed row's HOT background survives under the stripe (the stripe is laid
// OVER whatever the cell already wears, not instead of it) — asserted by the
// text still being painted at all.
func TestDiffSelectionOnAChangedRowStillPaints(t *testing.T) {
	prevP := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prevP)

	rows := []textdiff.Row{{Kind: textdiff.Changed, Left: "old-text", Right: "new-text", LeftNo: 1, RightNo: 1}}
	m := diffModel()
	m.width, m.height = 100, 6
	v := diffViewWith(rows, []int{0})
	v.relayout(m.width)
	v.lsel.start(0)
	v.lsel.mark(0)
	m = m.pushLayer(v)

	row := diffBodyRow(t, m, 0)
	right := row[strings.Index(row, "│"):]
	if sgrBefore(right, "new-text") == nil {
		t.Fatalf("the changed row's new cell is unpainted: %q", right)
	}
}

// feedDiff sends keys through the real Update loop (so the space
// normalization and the layer dispatch are exercised).
func feedDiff(m Model, keys ...string) Model {
	for _, k := range keys {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	return m
}
