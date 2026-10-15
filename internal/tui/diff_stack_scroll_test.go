package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// openManyFileStack opens the first file of a manyFileStackModel stacked.
func openManyFileStack(t *testing.T, n int) Model {
	t.Helper()
	m := manyFileStackModel(t, n)
	m = m.setStackedPref(true)
	var pick contentLine
	for _, l := range m.filesView.visible() {
		if l.path != "" {
			pick = l
			break
		}
	}
	u, cmd := m.openDiffForFileLine(pick)
	m = drainCmds(t, u.(Model), cmd)
	if v := m.diffLayer(); v == nil || v.stk == nil {
		t.Fatal("the diff must have opened stacked")
	}
	return m
}

// scrollStackUntil sends msg until file idx's header is the top body row.
func scrollStackUntil(t *testing.T, m Model, msg tea.Msg, idx int) Model {
	t.Helper()
	for range 500 {
		v := m.diffLayer()
		if v.offset >= v.lineStart[v.stk.files[idx].hdr] {
			return m
		}
		u, cmd := m.Update(msg)
		m = drainCmds(t, u.(Model), cmd)
	}
	t.Fatalf("file %d's header never reached the top of the view", idx)
	return m
}

// Scrolling a stack (↓ or the wheel) past the cursor's file brings the cursor
// along: the title, file N/M and every per-file key (h, b, H, notes) name the
// file being read, not one scrolled off the top. The user opened history for
// a file no longer on screen.
func TestStackScrollCarriesCursorIntoNextFile(t *testing.T) {
	t.Parallel()
	for name, msg := range map[string]tea.Msg{
		"down":  tea.KeyMsg{Type: tea.KeyDown},
		"wheel": tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := openManyFileStack(t, 5)
			if v := m.diffLayer(); v.curFile() != 0 {
				t.Fatalf("precondition: the cursor must start in file 0, it is in %d", v.curFile())
			}
			m = scrollStackUntil(t, m, msg, 1)
			v := m.diffLayer()
			want := v.stk.files[1].path
			if v.curFile() != 1 || v.title != want {
				t.Fatalf("file 1 is at the top of the view, yet the cursor is in file %d and the title is %q (want %q)",
					v.curFile(), v.title, want)
			}
			if !v.cursorVisible(m.diffBodyRows()) {
				t.Fatal("the cursor must be on screen after the scroll carried it")
			}
		})
	}
}

// A scroll that leaves the cursor on screen does not move it: the cursor is
// only carried once it would leave the view.
func TestStackScrollLeavesVisibleCursorAlone(t *testing.T) {
	t.Parallel()
	m := openManyFileStack(t, 5)
	for range 3 { // the cursor a few rows below the top, so one row of scroll keeps it on screen
		u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = drainCmds(t, u.(Model), cmd)
	}
	v := m.diffLayer()
	if start, _ := v.cursorDispRange(); start-v.offset < 1 {
		t.Fatalf("precondition: the cursor must sit below the top row, row %d offset %d", start, v.offset)
	}
	before := v.curLine
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = drainCmds(t, u.(Model), cmd)
	v = m.diffLayer()
	if v.offset == 0 {
		t.Fatal("precondition: ↓ must have scrolled the view")
	}
	if v.curLine != before {
		t.Fatalf("a one-row scroll with the cursor still on screen moved it: %d → %d", before, v.curLine)
	}
}

// Scrolling back up carries a cursor left below the view the same way: to the
// lowest line still in view, so the title names a file on screen.
func TestStackScrollUpCarriesCursorBack(t *testing.T) {
	t.Parallel()
	m := openManyFileStack(t, 5)
	m = scrollStackUntil(t, m, tea.KeyMsg{Type: tea.KeyDown}, 3)
	for range 500 {
		if m.diffLayer().offset == 0 {
			break
		}
		u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = drainCmds(t, u.(Model), cmd)
	}
	v := m.diffLayer()
	if v.curFile() > 1 || v.title != v.stk.files[v.curFile()].path || !v.cursorVisible(m.diffBodyRows()) {
		t.Fatalf("back at the top, the cursor is in file %d (title %q), visible=%v",
			v.curFile(), v.title, v.cursorVisible(m.diffBodyRows()))
	}
}

// Far into a big stack, files load as the scroll reaches them; each arrival
// rebuilds the stream, and the carried cursor must survive those rebuilds.
func TestStackScrollCarriedCursorSurvivesLoads(t *testing.T) {
	t.Parallel()
	m := openManyFileStack(t, 30)
	m = scrollStackUntil(t, m, tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}, 10)
	v := m.diffLayer()
	if v.curFile() != 10 || v.title != v.stk.files[10].path || !v.cursorVisible(m.diffBodyRows()) {
		t.Fatalf("file 10 is at the top, yet the cursor is in file %d (title %q), visible=%v",
			v.curFile(), v.title, v.cursorVisible(m.diffBodyRows()))
	}
}
