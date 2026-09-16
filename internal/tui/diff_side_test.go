package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
	"github.com/homeend/gigagit/internal/theme"
)

// NOTE: TestDiffCursorPaintsOneSide calls lipgloss.SetColorProfile
// (process-global) and therefore does NOT call t.Parallel().

// sideRows is a three-row file that exercises every cell shape: an unchanged
// row (both sides present), an Add row (the LEFT cell is absent — a gap) and a
// Del row (the RIGHT cell is absent).
func sideRows() []textdiff.Row {
	return []textdiff.Row{
		{Kind: textdiff.Same, Left: "same-left", Right: "same-left", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Add, Right: "added-right", RightNo: 2},
		{Kind: textdiff.Del, Left: "gone-left", LeftNo: 2},
	}
}

// sideModel opens a diff over sideRows with the cursor on the first row.
func sideModel(long longMode) Model {
	m := diffModel()
	m.width, m.height = 100, 8 // body = 6, so all three rows are visible
	v := diffViewWith(sideRows(), []int{1})
	v.long = long
	v.relayout(m.width)
	v.curLine = 0
	m = m.pushLayer(v)
	m.diffTag = "status:x"
	return m
}

// The band must land on the CURSOR side's cell only. The other cell renders as
// if this were not the cursor row. Asserted in all three long-line modes,
// because each has its own cell renderer (diffCell / scrollCell / segCell).
func TestDiffCursorPaintsOneSide(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	prevTheme := activeTheme()
	defer setTheme(prevTheme)
	setTheme(theme.Dark) // a theme with a real CursorRowBg, so the band has an SGR

	// The band's OPENING sequence, derived at runtime: sgrParams over the whole
	// render would also fold in the trailing reset, which no painted cell
	// carries before its text, so every assertion would miss by that one param.
	band := sgrBefore(st().diffCursorRow.Render("x"), "x")

	for _, lm := range []longMode{longScroll, longWrap, longTruncate} {
		m := sideModel(lm)
		v := m.diffLayer()

		// Default: the cursor is on the NEW (right) side.
		row := diffBodyRow(t, m, 0)
		if got := sgrBefore(row, "same-left"); subsetOf(band, got) {
			t.Errorf("mode %d: the LEFT cell must not wear the cursor band: %q", lm, row)
		}
		if got := sgrBefore(row, "same-left"); got == nil {
			t.Errorf("mode %d: the left cell text is missing from %q", lm, row)
		}
		// The right cell holds the same text, so find the SECOND occurrence.
		right := row[strings.Index(row, "│"):]
		if got := sgrBefore(right, "same-left"); !subsetOf(band, got) {
			t.Errorf("mode %d: the RIGHT cell must wear the cursor band, params %v: %q", lm, got, right)
		}

		// alt+left flips the side: the band moves to the left cell.
		u, _ := m.Update(keyMsg("alt+left"))
		m = u.(Model)
		if !m.diffLayer().onOld {
			t.Fatalf("mode %d: alt+left must put the cursor on the old side", lm)
		}
		row = diffBodyRow(t, m, 0)
		if got := sgrBefore(row, "same-left"); !subsetOf(band, got) {
			t.Errorf("mode %d: after alt+left the LEFT cell must wear the band, params %v: %q", lm, got, row)
		}
		right = row[strings.Index(row, "│"):]
		if got := sgrBefore(right, "same-left"); subsetOf(band, got) {
			t.Errorf("mode %d: after alt+left the RIGHT cell must not wear the band: %q", lm, right)
		}

		// The GAP cell on the cursor side still wears the band: on the Add row
		// (display row 1) the left side has no line, and the user must still be
		// able to see where the cursor is.
		v = m.diffLayer()
		v.curLine = 1 // the Add row; the cursor is still on the old (left) side
		gapRow := diffBodyRow(t, m, 1)
		gapSeq := sgrBefore(gapRow, "·")
		bandBg := sgrBefore(st().diffGapCursor.Render("·"), "·")
		if !subsetOf(bandBg, gapSeq) {
			t.Errorf("mode %d: the gap cell on the cursor side must wear the cursor tint, params %v: %q", lm, gapSeq, gapRow)
		}

		// alt+right returns to the new side.
		u, _ = m.Update(keyMsg("alt+right"))
		if u.(Model).diffLayer().onOld {
			t.Fatalf("mode %d: alt+right must put the cursor back on the new side", lm)
		}
	}
}

// The header's line number names the CURSOR side: "line N" on the right,
// "old line N" on the left, falling back to the other side's number (with that
// side's label) when the cursor cell is a gap.
func TestDiffHeaderNamesTheCursorSide(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	head := strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "line 1") || strings.Contains(head, "old line 1") {
		t.Fatalf("on the new side the header must say `line 1`: %q", head)
	}
	u, _ := m.Update(keyMsg("alt+left"))
	m = u.(Model)
	head = strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "old line 1") {
		t.Fatalf("on the old side the header must say `old line 1`: %q", head)
	}
	// Cursor on the Add row (index 1) with the side still old: the left cell is
	// a gap, so the header falls back to the right side's number and label.
	m.diffLayer().curLine = 1
	head = strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "line 2") || strings.Contains(head, "old line 2") {
		t.Fatalf("a gap cell must fall back to the other side's label: %q", head)
	}
}

// The note anchors — hence the popup's default pick, noteAnchorAtCursor, the L
// key and the . menu's Copy link row — follow the cursor side.
func TestNoteAnchorFollowsCursorSide(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	v := m.diffLayer()
	v.setCursorLine(10, m.diffBodyRows()) // a Same row: both sides exist

	side, line, _, ok := m.noteAnchorAtCursor()
	if !ok || side != model.NoteSideNew || line != v.lines[10].Row.RightNo {
		t.Fatalf("on the new side the default anchor = %v/%d, want new/%d", side, line, v.lines[10].Row.RightNo)
	}

	v.onOld = true
	side, line, _, ok = m.noteAnchorAtCursor()
	if !ok || side != model.NoteSideOld || line != v.lines[10].Row.LeftNo {
		t.Fatalf("on the old side the default anchor = %v/%d, want old/%d", side, line, v.lines[10].Row.LeftNo)
	}
	// Both sides are still OFFERED, just reordered.
	if as := m.noteAnchorsAtCursor(); len(as) != 2 || as[1].side != model.NoteSideNew {
		t.Fatalf("the picker must still list both sides, new second: %+v", as)
	}
	// The gg:// link the L key copies takes the old side too.
	m.linkRepoName = "gigagit"
	m.currentWorktree = "/wt"
	got, ok := m.contextLinkText()
	if !ok || !strings.Contains(got, ":old:") {
		t.Fatalf("contextLinkText on the old side = %q ok=%v, want an :old: link", got, ok)
	}
}

// A live selection LOCKS the side: alt+←/→ leaves onOld alone and posts the
// bottom-left notice instead, so the range can never mix the two sides' text.
func TestDiffAltArrowsLockedWhileSelecting(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	v := m.diffLayer()
	v.lsel.start(v.curLine)

	u, _ := m.Update(keyMsg("alt+left"))
	m = u.(Model)
	if m.diffLayer().onOld {
		t.Fatal("alt+left must be ignored while a selection is on")
	}
	if !strings.Contains(m.diffNotice, "locked to the new side") {
		t.Fatalf("diffNotice = %q, want the new-side lock notice", m.diffNotice)
	}

	m.diffLayer().onOld = true
	u, _ = m.Update(keyMsg("alt+right"))
	m = u.(Model)
	if !m.diffLayer().onOld {
		t.Fatal("alt+right must be ignored while a selection is on")
	}
	if !strings.Contains(m.diffNotice, "locked to the old side") {
		t.Fatalf("diffNotice = %q, want the old-side lock notice", m.diffNotice)
	}
}

// A left click places the cursor on the row AND on the pane it landed in;
// while a selection is live only the row moves (the side is locked).
//
// The message shape is the package idiom (action_menu_click_test.go:49):
// handleMouse returns immediately unless msg.Action == tea.MouseActionPress,
// and then dispatches on msg.Button — it never reads a Type field.
func TestDiffClickPicksThePane(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	paneW := (m.width - 1) / 2

	u, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: 1})
	m = u.(Model)
	if !m.diffLayer().onOld {
		t.Fatal("a click in the left pane must put the cursor on the old side")
	}
	u, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: paneW + 5, Y: 1})
	m = u.(Model)
	if m.diffLayer().onOld {
		t.Fatal("a click in the right pane must put the cursor on the new side")
	}

	m.diffLayer().lsel.start(0)
	u, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: 3})
	m = u.(Model)
	if m.diffLayer().onOld {
		t.Fatal("while a selection is on the click must not flip the side")
	}
	if m.diffLayer().curLine != 2 {
		t.Fatalf("the click must still move the row, curLine = %d want 2", m.diffLayer().curLine)
	}
}

// A reload (diffMsg) must not flip the user's side out from under them: the
// side is carried across `*dv = *msg.view` exactly like the search is.
func TestDiffMsgCarriesTheCursorSide(t *testing.T) {
	t.Parallel()
	m := sideModel(longScroll)
	m.diffLayer().onOld = true
	m.diffLayer().lsel.start(0)

	fresh := diffViewWith(sideRows(), []int{1})
	u, _ := m.Update(diffMsg{tag: m.diffTag, view: fresh})
	m = u.(Model)
	if !m.diffLayer().onOld {
		t.Fatal("a reload must keep the cursor on the side the user chose")
	}
	if m.diffLayer().lsel.on {
		t.Fatal("a reload replaces the line stream: the selection must be gone")
	}
}

// sidePresent and cursorCell are what Task 3's copy/selection reads: the
// cursor side's SOURCE text and number, and nothing at all on a gap cell.
func TestCursorCellReadsTheCursorSide(t *testing.T) {
	t.Parallel()
	v := diffViewWith(sideRows(), []int{1})
	for _, tc := range []struct {
		line       int
		onOld      bool
		text       string
		no         int
		ok         bool
		wantSidePr bool
	}{
		{0, false, "same-left", 1, true, true},   // Same: both sides present
		{0, true, "same-left", 1, true, true},    //
		{1, false, "added-right", 2, true, true}, // Add: the new side has the line
		{1, true, "", 0, false, false},           // …and the old side is a gap
		{2, false, "", 0, false, false},          // Del: the new side is a gap
		{2, true, "gone-left", 2, true, true},    // …and the old side has the line
	} {
		v.curLine, v.onOld = tc.line, tc.onOld
		if got := sidePresent(v.lines[tc.line].Row, tc.onOld); got != tc.wantSidePr {
			t.Errorf("sidePresent(line %d, onOld=%v) = %v, want %v", tc.line, tc.onOld, got, tc.wantSidePr)
		}
		text, no, ok := v.cursorCell()
		if text != tc.text || no != tc.no || ok != tc.ok {
			t.Errorf("cursorCell(line %d, onOld=%v) = %q/%d/%v, want %q/%d/%v",
				tc.line, tc.onOld, text, no, ok, tc.text, tc.no, tc.ok)
		}
	}
}

// diffBodyRow renders the diff and returns body row i (row 0 of the panes,
// which is screen line 1 — line 0 is the header).
func diffBodyRow(t *testing.T, m Model, i int) string {
	t.Helper()
	lines := strings.Split(m.renderDiffView(), "\n")
	if i+1 >= len(lines) {
		t.Fatalf("body row %d is past the render (%d lines)", i, len(lines))
	}
	return lines[i+1]
}

// subsetOf reports whether every parameter of want is present in got. SGR
// sequences pack several attributes together, so an exact-set comparison would
// break the moment an unrelated attribute joins the run.
func subsetOf(want, got map[string]bool) bool {
	if got == nil || len(want) == 0 {
		return false
	}
	for k := range want {
		if !got[k] {
			return false
		}
	}
	return true
}
