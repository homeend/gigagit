package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
)

func click(t *testing.T, m Model, x, y int) Model {
	t.Helper()
	u, cmd := m.Update(mouseMsg(x, y, tea.MouseButtonLeft))
	return drainCmds(t, u.(Model), cmd)
}

func TestSplitClickFocusesTheSideUnderThePointer(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	lw, _, _ := m.splitGeom()
	m = click(t, m, lw+5, 5)
	if sv.onLeft {
		t.Fatal("a click in the right box focuses it")
	}
	click(t, m, 5, 5)
	if !sv.onLeft {
		t.Fatal("a click in the left box focuses it")
	}
}

func TestSplitClickOnTheFilePlacesTheCursor(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	lw, _, _ := m.splitGeom()
	d := sv.right.doc()
	click(t, m, lw+5, 2) // the first content row (border, title above it)
	if d.p.cur != d.p.sel {
		t.Fatalf("cur %d top %d: the click lands the cursor on the row clicked", d.p.cur, d.p.sel)
	}
}

func TestSplitClickOnAnAnchorSelectsAndSteersDoubleOpens(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	ov := sv.left.doc()
	s := ov.ov.anchors[1].spans[0] // "B" in "Start at A, then B, …"
	bx, by := 2+s.from, 2+s.line   // border + padding; border + title
	m = click(t, m, bx, by)
	if ov.ov.sel != 1 || sv.right.doc().path != "b.go" || !sv.onLeft {
		t.Fatalf("sel %d right %s onLeft %v", ov.ov.sel, sv.right.doc().path, sv.onLeft)
	}
	click(t, m, bx, by)
	if sv.onLeft {
		t.Fatal("a second click on the anchor opens it: focus right")
	}
}

func TestSplitWheelScrollsTheSideUnderThePointerWithoutFocus(t *testing.T) {
	t.Parallel()
	m, sv := reviewSplitModel(t)
	lw, _, _ := m.splitGeom()
	sv.right.(*docPane).d = splitDoc("long.go", "x", 200) // something to scroll
	u, _ := m.Update(mouseMsg(lw+5, 5, tea.MouseButtonWheelDown))
	m = u.(Model)
	if sv.right.doc().p.sel == 0 || !sv.onLeft {
		t.Fatalf("right sel %d onLeft %v", sv.right.doc().p.sel, sv.onLeft)
	}
	_ = m
}

// A wrapped line takes several rows: a click under it lands on the line
// drawn there, not on the line whose index equals the row.
func TestSplitClickInWrapModeLandsOnTheLineDrawnThere(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	lw, _, _ := m.splitGeom()
	d := sv.right.doc()
	d.p.lines[0].text = strings.Repeat("word ", 40) // ~200 cells: several rows at 56
	d.p.mode = modeWrap
	m.View()
	_, inner := sv.geomOf(m, sv.right)
	segs, _ := wrapRow(d.p.lines[0].text, inner, winOpts{w: inner, mode: modeWrap, charWrap: true}, 0)
	if len(segs) < 2 {
		t.Fatalf("line 1 must wrap: %d segments", len(segs))
	}
	m = click(t, m, lw+5, 2+len(segs)) // the first row after line 1's segments
	if d.p.cur != 1 {
		t.Fatalf("cur %d, want 1 (line 2 is drawn under line 1's %d rows)", d.p.cur, len(segs))
	}
	click(t, m, lw+5, 2+len(segs)-1) // line 1's last segment
	if d.p.cur != 0 {
		t.Fatalf("cur %d, want 0 (a continuation row belongs to its line)", d.p.cur)
	}
}

// A note box hangs under its line as rows that are not lines: a click below
// it lands on the line drawn there, a click on the box on the line it
// belongs to.
func TestSplitClickUnderANoteBoxLandsOnTheLineDrawnThere(t *testing.T) {
	t.Parallel()
	m, sv := testSplit(t)
	lw, _, _ := m.splitGeom()
	d := sv.right.doc()
	d.notes = []agentdocs.Note{{ID: "t-1", Seq: 1, Start: 1, End: 1, Summary: "a note", Author: "agent"}}
	d.syncNoteRows()
	m.View()
	box := d.noteRowsUnder(0, 1)
	if box == 0 {
		t.Fatal("the note draws a box")
	}
	m = click(t, m, lw+5, 2+1+box) // the row after line 1 and its box
	if d.p.cur != 1 {
		t.Fatalf("cur %d, want 1 (line 2 sits under the %d-row box)", d.p.cur, box)
	}
	click(t, m, lw+5, 2+1) // the box's first row
	if d.p.cur != 0 {
		t.Fatalf("cur %d, want 0 (the box belongs to line 1)", d.p.cur)
	}
}
