package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
