package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// rangeNotePopup presses c and returns the form it opened (nil: none).
func rangeNotePopup(t *testing.T, m Model) (Model, *notePopup) {
	t.Helper()
	u, _ := m.Update(keyMsg("c"))
	m = u.(Model)
	p, _ := m.topLayer().(*notePopup)
	return m, p
}

// With lines marked, c writes ONE note over them: the cursor's side, first to
// last line, fingerprinted over the whole block, and no side to choose.
func TestCWithMarkedLinesOpensARangeNote(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateUnstaged)
	v := m.diffLayer()
	selectRows(v, 4, 6)
	m, p := rangeNotePopup(t, m)
	if p == nil {
		t.Fatal("c with lines marked opened no form")
	}
	if p.side != model.NoteSideNew || p.first != 5 || p.line != 7 {
		t.Fatalf("anchor = %s %d-%d, want new 5-7", p.side, p.first, p.line)
	}
	if want := model.NoteContextHash([]string{"l05", "l06", "l07"}); p.hash != want {
		t.Errorf("hash = %q, want the block's", p.hash)
	}
	if p.hasSideField() {
		t.Error("a range note must not offer a side: the marks are on one side")
	}
	if box := p.box(m); !strings.Contains(box, "new side lines 5-7") {
		t.Errorf("the form does not name the range:\n%s", box)
	}
	if n := p.note("s", "r"); n.Range != [2]int{5, 7} || n.Side != model.NoteSideNew || n.ContextHash != p.hash {
		t.Errorf("note = %+v, want range 5-7 on the new side", n)
	}

	// The old side.
	m = rangeLinkModel(t, model.StateUnstaged)
	v = m.diffLayer()
	selectRows(v, 4, 6)
	v.onOld = true
	if _, p = rangeNotePopup(t, m); p == nil || p.side != model.NoteSideOld || p.first != 5 || p.line != 7 {
		t.Fatalf("old side: popup = %+v", p)
	}
}

// Rows with no line on the cursor's side are trimmed at the edges, as L does.
func TestRangeNoteTrimsGapRows(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateUnstaged)
	selectRows(m.diffLayer(), 20, 23) // del-only row, add row (new 21), new 22, new 23
	_, p := rangeNotePopup(t, m)
	if p == nil || p.first != 21 || p.line != 23 {
		t.Fatalf("popup = %+v, want new 21-23", p)
	}
	if want := model.NoteContextHash([]string{"added", "l22", "l23"}); p.hash != want {
		t.Errorf("hash is not the trimmed block's")
	}
}

// One marked row is today's one-line note at the cursor, side choice included.
func TestOneMarkedLineIsAOneLineNote(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateUnstaged)
	selectRows(m.diffLayer(), 4, 4)
	_, p := rangeNotePopup(t, m)
	if p == nil || p.first != p.line || p.line != 5 {
		t.Fatalf("popup = %+v, want the one line 5", p)
	}
	if !p.hasSideField() {
		t.Error("a one-line note on a two-sided row keeps its side choice")
	}
	if n := p.note("s", ""); n.Range != [2]int{5, 5} {
		t.Errorf("range = %v", n.Range)
	}
}

func TestRangeNoteRefusals(t *testing.T) {
	t.Parallel()
	// No line on the cursor's side anywhere in the marks.
	rows := sameRowsTUI(12)
	rows[5] = textdiff.Row{Kind: textdiff.Del, Left: "a", LeftNo: 6}
	rows[6] = textdiff.Row{Kind: textdiff.Del, Left: "b", LeftNo: 7}
	m := openedDiffModel(20, rows, []int{5})
	v := m.diffLayer()
	v.noteAddr = model.FileAddress{State: model.StateUnstaged, Path: "a.txt", Worktree: "/repo"}
	selectRows(v, 5, 6)
	m, p := rangeNotePopup(t, m)
	if p != nil {
		t.Fatal("marks with no line on this side opened a form")
	}
	if !strings.Contains(m.diffNotice, "nothing to note on this side") {
		t.Errorf("notice = %q", m.diffNotice)
	}

	// A preview's old side is the merge base: no note anchors there.
	m = rangeLinkModel(t, model.StateUnstaged)
	v = m.diffLayer()
	v.previewSet = &domain.PreviewNoteSet{}
	selectRows(v, 4, 6)
	v.onOld = true
	m, p = rangeNotePopup(t, m)
	if p != nil {
		t.Fatal("the old side of a preview opened a form")
	}
	if !strings.Contains(m.diffNotice, "notes in a preview anchor on the new side") {
		t.Errorf("notice = %q", m.diffNotice)
	}
}

// In a stack the marks may stay in one file while the cursor walks to another:
// the note belongs to the file of the MARKS; marks across files are refused.
func TestRangeNoteInAStack(t *testing.T) {
	t.Parallel()
	r := sameRowsTUI(6)
	v := stackViewOf(t, r, r)
	for i := range v.stk.files {
		v.stk.files[i].d.noteAddr = model.FileAddress{State: model.StateUnstaged, Path: v.stk.files[i].path, Worktree: "/repo"}
	}
	line := func(file, no int) int {
		for i, l := range v.lines {
			if l.file == file && l.isBody() && l.Row.RightNo == no {
				return i
			}
		}
		t.Fatalf("no row for file %d line %d", file, no)
		return -1
	}
	m := diffModel().pushLayer(v)
	selectRows(v, line(1, 2), line(1, 4))
	v.curLine = line(0, 3) // frozen marks in file 1, cursor in file 0
	_, p := rangeNotePopup(t, m)
	if p == nil || p.addr.Path != v.stk.files[1].path || p.first != 2 || p.line != 4 {
		t.Fatalf("popup = %+v, want lines 2-4 of %s", p, v.stk.files[1].path)
	}
	if v.curLine != line(0, 3) {
		t.Error("opening the form moved the cursor")
	}

	m = diffModel().pushLayer(v)
	selectRows(v, line(0, 5), line(1, 2))
	m, p = rangeNotePopup(t, m)
	if p != nil {
		t.Fatal("marks across two files opened a form")
	}
	if !strings.Contains(m.diffNotice, "a note marks lines of one file") {
		t.Errorf("notice = %q", m.diffNotice)
	}
}

// Saving the note clears the marks; leaving the form keeps them.
func TestRangeNoteSaveClearsTheMarks(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateUnstaged)
	v := m.diffLayer()
	selectRows(v, 4, 6)
	m, p := rangeNotePopup(t, m)
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if !v.lsel.on {
		t.Fatal("esc in the form dropped the marks")
	}
	m, p = rangeNotePopup(t, m)
	p.summary = newTextField("why")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if v.lsel.on {
		t.Error("saving the note kept the marks")
	}
	_ = m
}

func TestSelectionHintOffersTheNote(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"[c] note", "[L] link"} {
		if !strings.Contains(diffSelectHint(), want) {
			t.Errorf("selection hint lacks %q: %s", want, diffSelectHint())
		}
	}
}
