package tui

import (
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// bigStackPane builds the stacked file pane over 101 files + a.go (the
// overview's target, last), so every other file opens folded by the stream
// itself and the pane opens at the stream's end.
func bigStackPane(t *testing.T) (Model, *splitView, *stackPane) {
	t.Helper()
	m, sv := reviewSplitModel(t)
	var many []contentLine
	for i := 0; i < 101; i++ {
		many = append(many, contentLine{path: "f" + strconv.Itoa(i) + ".go", status: "M"})
	}
	m.filesView.lines = append(append([]contentLine{}, many...), contentLine{path: "a.go", status: "M"})
	sp := m.newStackPane(sv).(*stackPane)
	sv.right = sp
	sv.focus(false)
	sp.pump(m) // what every opening path does
	return m, sv, sp
}

func pressPane(m Model, sp *stackPane, k string) Model {
	nm, _ := sp.update(m, key(k))
	return nm
}

// The stream unfolds every file on screen — at open, and after every page —
// and a file never shown stays folded.
func TestStackPaneUnfoldsEveryFileOnScreen(t *testing.T) {
	t.Parallel()
	m, _, sp := bigStackPane(t)
	check := func(when string) {
		for i := range sp.visibleHeaders(m) {
			if sp.stream.files[i].folded {
				t.Fatalf("%s: file %d is on screen and still folded", when, i)
			}
		}
		if !sp.stream.files[0].folded {
			t.Fatalf("%s: a file never shown must stay folded", when)
		}
	}
	check("at open")
	m = pressPane(m, sp, "pgup")
	check("after pgup")
}

// The cursor moving onto a folded header unfolds that file.
func TestStackPaneCursorOntoAFoldedHeaderUnfoldsIt(t *testing.T) {
	t.Parallel()
	m, _, sp := bigStackPane(t)
	pressPane(m, sp, "P")
	if fi := sp.stream.fileAt(sp.d.p.cur); fi != 100 || sp.stream.files[100].hdr != sp.d.p.cur {
		t.Fatalf("P must stop on f100's header (file %d)", fi)
	}
	if sp.stream.files[100].folded {
		t.Fatal("the cursor landed on a folded header: the file must unfold")
	}
}

// A file folded by hand stays folded when it comes back into view.
func TestStackPaneFileFoldedByHandStaysFolded(t *testing.T) {
	t.Parallel()
	m, _, sp := bigStackPane(t)
	m = pressPane(m, sp, "P") // onto f100: it unfolds
	m = pressPane(m, sp, "-") // …and the reader folds it again
	if !sp.stream.files[100].folded {
		t.Fatal("- must fold the file under the cursor")
	}
	m = pressPane(m, sp, "pgup")
	m = pressPane(m, sp, "pgdown")
	m = pressPane(m, sp, "N")
	m = pressPane(m, sp, "P")
	if !sp.stream.files[100].folded {
		t.Fatal("a file folded by hand must stay folded when it comes back into view")
	}
}

// The wheel unfolds what it scrolls into view, as a page does.
func TestStackPaneWheelUnfoldsWhatItScrollsIn(t *testing.T) {
	t.Parallel()
	m, sv, sp := bigStackPane(t)
	before := sp.visibleHeaders(m)
	lw, _, _ := m.splitGeom()
	m, _ = m.splitMouse(sv, tea.MouseMsg{X: lw + 4, Y: 5}, -40)
	now := sp.visibleHeaders(m)
	moved := false
	for i := range now {
		moved = moved || !before[i]
		if sp.stream.files[i].folded {
			t.Fatalf("file %d scrolled into view and is still folded", i)
		}
	}
	if !moved {
		t.Fatal("the wheel must have brought other files on screen")
	}
}

// pgup scrolls the pane away from its cursor: the files it unfolds — and
// the loads that land for them — must not pull the view back to the cursor.
func TestStackPaneUnfoldKeepsAViewScrolledAwayFromTheCursor(t *testing.T) {
	t.Parallel()
	m, _, sp := bigStackPane(t)
	top := sp.d.p.sel
	m = pressPane(m, sp, "pgup")
	if sp.d.p.sel >= top {
		t.Fatalf("pgup must leave the view scrolled up (top %d → %d)", top, sp.d.p.sel)
	}
	first := -1
	for i := range sp.visibleHeaders(m) {
		if first < 0 || i < first {
			first = i
		}
	}
	if first < 0 || sp.stream.files[first].folded {
		t.Fatalf("the page must show the files it unfolded (first header %d)", first)
	}
	// a file of the page arrives: the view stays on the page
	was := sp.d.p.sel - sp.stream.files[first].start
	m, _ = m.stackPaneLanded(stackPaneMsg{tag: sp.d.tag, gen: sp.gen, file: first, lines: []contentLine{{text: "x", src: true}, {text: "y", src: true}}})
	if got := sp.d.p.sel - sp.stream.files[first].start; got != was {
		t.Fatalf("a load on the page moved the view: %d rows from its file's start, was %d", got, was)
	}
}

// A file's remarks landing on a page scrolled away from the cursor leave the
// page where it is.
func TestStackPaneRemarksLandingKeepAScrolledView(t *testing.T) {
	t.Parallel()
	m, _, sp := bigStackPane(t)
	m = pressPane(m, sp, "pgup")
	top := sp.d.p.sel
	m = m.stackNotesLanded(splitStackNotesMsg{tag: sp.d.tag, gen: sp.gen, file: 99})
	if sp.d.p.sel != top {
		t.Fatalf("remarks landing moved a view scrolled away from the cursor: top %d → %d", top, sp.d.p.sel)
	}
}
