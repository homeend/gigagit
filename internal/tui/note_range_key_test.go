package tui

import (
	"errors"
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

	// Loose marks (one space) follow the cursor.
	m = rangeLinkModel(t, model.StateUnstaged)
	v = m.diffLayer()
	v.lsel = lineSel{on: true, anchor: 4}
	v.curLine = 6
	if _, p = rangeNotePopup(t, m); p == nil || p.first != 5 || p.line != 7 {
		t.Fatalf("loose marks: popup = %+v, want 5-7", p)
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
	if !strings.Contains(m.diffNotice, "nothing to note on this side — [esc] unmark") {
		t.Errorf("notice = %q", m.diffNotice)
	}

	// A preview's old side is the merge base: no note anchors there.
	m = rangeLinkModel(t, model.StateUnstaged)
	v = m.diffLayer()
	v.previewSet = &domain.PreviewNoteSet{Source: "feat", Target: "main"}
	selectRows(v, 4, 6)
	v.onOld = true
	m, p = rangeNotePopup(t, m)
	if p != nil {
		t.Fatal("the old side of a preview opened a form")
	}
	if !strings.Contains(m.diffNotice, "notes in a preview anchor on the new side") {
		t.Errorf("notice = %q", m.diffNotice)
	}

	// A commit pair (a set with no branch names) is a compare: the refusal
	// names the view on screen.
	m = rangeLinkModel(t, model.StateUnstaged)
	v = m.diffLayer()
	v.previewSet = &domain.PreviewNoteSet{Tip: "b", Base: "a"}
	selectRows(v, 4, 6)
	v.onOld = true
	if m, p = rangeNotePopup(t, m); p != nil || !strings.Contains(m.diffNotice, "notes in a compare anchor on the new side") {
		t.Errorf("pair old side: popup=%v notice=%q", p != nil, m.diffNotice)
	}
}

// Editing or replying to a note over several lines names the range, as the
// add form did.
func TestEditAndReplyHeadingNameTheRange(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateUnstaged)
	tgt := noteTarget{rootID: "n1", first: 5, line: 7, side: model.NoteSideNew, note: model.Note{ID: "n1", Summary: "s"}}
	for _, mode := range []noteFormMode{noteEdit, noteReply} {
		u, _ := m.openNotePopupFor(mode, tgt)
		p, _ := u.(Model).topLayer().(*notePopup)
		if p == nil {
			t.Fatalf("mode %d opened no form", mode)
		}
		if box := p.box(u.(Model)); !strings.Contains(box, "new side lines 5-7") {
			t.Errorf("mode %d: heading does not name the range:\n%s", mode, box)
		}
	}
	tgt.first = 0 // an older caller: one line
	u, _ := m.openNotePopupFor(noteEdit, tgt)
	if box := u.(Model).topLayer().(*notePopup).box(u.(Model)); !strings.Contains(box, "new side line 7") {
		t.Errorf("one-line heading:\n%s", box)
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
	if !v.lsel.on {
		t.Fatal("the marks went before the write was known to succeed")
	}
	// A failed write keeps the marks (the text is lost, the range is not).
	u, _ := m.Update(noteMutatedMsg{err: errTestNote, clearMarks: true})
	if m = u.(Model); !v.lsel.on {
		t.Fatal("a failed write dropped the marks")
	}
	u, _ = m.Update(noteMutatedMsg{clearMarks: true})
	if v.lsel.on {
		t.Error("saving the note kept the marks")
	}
	_ = u
}

var errTestNote = errors.New("boom")

// One frozen mark with the cursor walked away: the note goes to the MARK (as
// L's link does), not to the cursor.
func TestOneFrozenMarkAwayFromTheCursorTakesTheNote(t *testing.T) {
	t.Parallel()
	m := rangeLinkModel(t, model.StateUnstaged)
	v := m.diffLayer()
	selectRows(v, 4, 4)
	v.curLine = 9
	_, p := rangeNotePopup(t, m)
	if p == nil || p.first != 5 || p.line != 5 || !p.ranged || p.hasSideField() {
		t.Fatalf("popup = %+v, want the marked line 5", p)
	}
}

// In a stack the gates read the file of the MARKS: a cursor parked in a file
// that takes no notes does not silence c, and marks in such a file are inert.
func TestRangeNoteGatesOnTheMarksFile(t *testing.T) {
	t.Parallel()
	r := sameRowsTUI(6)
	build := func(stamped int) (Model, *diffView, func(file, no int) int) {
		v := stackViewOf(t, r, r)
		v.stk.files[stamped].d.noteAddr = model.FileAddress{State: model.StateUnstaged, Path: v.stk.files[stamped].path, Worktree: "/repo"}
		line := func(file, no int) int {
			for i, l := range v.lines {
				if l.file == file && l.isBody() && l.Row.RightNo == no {
					return i
				}
			}
			t.Fatalf("no row for file %d line %d", file, no)
			return -1
		}
		return diffModel().pushLayer(v), v, line
	}
	m, v, line := build(1)
	selectRows(v, line(1, 2), line(1, 4))
	v.curLine = line(0, 3) // the cursor's file has no address
	if _, p := rangeNotePopup(t, m); p == nil || p.first != 2 || p.line != 4 {
		t.Fatalf("popup = %+v, want lines 2-4 of the marks' file", p)
	}
	m, v, line = build(0)
	selectRows(v, line(1, 2), line(1, 4))
	v.curLine = line(0, 3) // the marks' file has no address
	if _, p := rangeNotePopup(t, m); p != nil {
		t.Fatalf("marks in a file with no address opened a form: %+v", p)
	}
}

func TestSelectionHintOffersTheNote(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"[c] note", "[L] link"} {
		if !strings.Contains(diffSelectHint(), want) {
			t.Errorf("selection hint lacks %q: %s", want, diffSelectHint())
		}
	}
}
