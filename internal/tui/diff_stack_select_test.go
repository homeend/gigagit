package tui

import (
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// stackSelModel is a stack of three files — the first not loaded yet — with a
// FIXED selection from file 1's fourth line to file 2's fourth line. It
// returns the lines that selection copies.
func stackSelModel(t *testing.T) (Model, []string) {
	t.Helper()
	v := stackViewOf(t, nil, sameRowsTUI(40, 5), sameRowsTUI(40, 5))
	m := diffModel()
	m.height, m.width = 12, 120
	m = m.pushLayer(v)
	dv := m.diffLayer()
	body := m.diffBodyRows()
	dv.setCursorLine(dv.stk.files[1].start+3, body)
	dv.lsel.press(dv.curLine)
	dv.setCursorLine(dv.stk.files[2].start+3, body)
	dv.lsel.press(dv.curLine)
	want := dv.selectedLines()
	if len(want) < 2 {
		t.Fatalf("the fixture selection copies %d lines", len(want))
	}
	return m, want
}

func sameSel(t *testing.T, m Model, want []string, what string) {
	t.Helper()
	dv := m.diffLayer()
	if !dv.lsel.on {
		t.Fatalf("%s wiped the selection", what)
	}
	if got := dv.selectedLines(); !slices.Equal(got, want) {
		t.Fatalf("%s moved the selection: %d lines → %d", what, len(want), len(got))
	}
}

// Files load on their own as the reader scrolls; one arriving ABOVE the
// selection shifts every line index below it, and the range must follow its
// lines, not the indexes.
func TestStackSelectionSurvivesALateLoad(t *testing.T) {
	t.Parallel()
	m, want := stackSelModel(t)
	dv := m.diffLayer()
	u, _ := m.Update(stackFileMsg{gen: dv.stk.gen, idx: 0, view: diffViewWith(sameRowsTUI(30, 3), []int{3})})
	sameSel(t, u.(Model), want, "a file loading above")
}

// Folding a file out of the way is how a reader shortens a long range.
func TestStackSelectionSurvivesFoldingAnotherFile(t *testing.T) {
	t.Parallel()
	m, want := stackSelModel(t)
	dv := m.diffLayer()
	m = m.foldFile(dv, 0, m.diffBodyRows())
	sameSel(t, m, want, "folding another file")
}

// A file's review notes arrive right after its rows, and rebuild the stream.
func TestStackSelectionSurvivesNotesArriving(t *testing.T) {
	t.Parallel()
	m, want := stackSelModel(t)
	dv := m.diffLayer()
	u, _ := m.Update(stackNotesMsg{gen: dv.stk.gen, idx: 1})
	sameSel(t, u.(Model), want, "notes arriving")
}

// A working-tree refresh renumbers the files: the selection follows its file
// by path, and goes when the file leaves the section.
func TestStackSelectionFollowsItsFileAcrossAReconcile(t *testing.T) {
	t.Parallel()
	m := statusStackModel(t)
	v := m.diffLayer()
	v.stk.files[1].d, v.stk.files[1].load = diffViewWith(sameRowsTUI(12, 1), []int{1}), stackLoaded
	v.rebuild()
	body := m.diffBodyRows()
	v.setCursorLine(v.stk.files[1].start+3, body)
	v.lsel.press(v.curLine)
	v.setCursorLine(v.stk.files[1].start+6, body)
	v.lsel.press(v.curLine)
	want := v.selectedLines()

	m = m.withStatus(model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "0new.txt", Unstaged: 'M'},
		{Path: "a.txt", Unstaged: 'M'},
		{Path: "b.txt", Unstaged: 'M'},
	}})
	sameSel(t, m, want, "a refresh that inserts a file above")

	m = m.withStatus(model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "0new.txt", Unstaged: 'M'},
		{Path: "a.txt", Unstaged: 'M'},
	}})
	if m.diffLayer().lsel.on {
		t.Fatal("the selection must go with its file")
	}
}

// f changes what a file's lines ARE (folded runs appear or open), so the
// indexes mean nothing afterwards — single-file parity: it clears.
func TestStackFStillClearsTheSelection(t *testing.T) {
	t.Parallel()
	m, _ := stackSelModel(t)
	u, _ := m.Update(keyMsg("f"))
	if u.(Model).diffLayer().lsel.on {
		t.Fatal("f must clear a stacked selection")
	}
}

// Folding a file that holds an end parks that end on the header; unfolding it
// again must bring the range back whole, not leave it shrunk to the headers.
func TestStackSelectionComesBackAfterFoldingItsOwnFiles(t *testing.T) {
	t.Parallel()
	m, want := stackSelModel(t)
	u, _ := m.Update(keyMsg("_")) // fold everything
	u, _ = u.(Model).Update(keyMsg("_"))
	sameSel(t, u.(Model), want, "fold all + unfold all")

	m, want = stackSelModel(t)
	dv := m.diffLayer()
	body := m.diffBodyRows()
	m = m.foldFile(dv, 1, body)
	m = m.foldFile(m.diffLayer(), 1, body)
	sameSel(t, m, want, "folding and unfolding the anchor's file")
}
