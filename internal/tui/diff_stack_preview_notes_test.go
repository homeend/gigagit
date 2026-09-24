package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A merge preview's notes live at the source TIP. The single-file opener
// stamps that address on the layer and the compare loader inherits it from
// the layer on top — but in a stack the layer on top is the STACK, so every
// file but the one opened arrived unaddressed and showed no notes. Each file
// the stack lands must carry its own address.
func TestStackPreviewFilesCarryTheirOwnNoteAddress(t *testing.T) {
	t.Parallel()
	const tip = "1111111111111111111111111111111111111111"
	set := &domain.PreviewNoteSet{Source: "feat", Target: "main", Tip: tip, Commits: []string{tip}}
	v := stackViewOf(t, sameRowsTUI(10, 2), nil)
	m := diffModel()
	m.height, m.width = 20, 120
	m.filesMode = filesModeCompare
	m.filesPreviewSet = set
	m = m.pushLayer(v)
	dv := m.diffLayer()

	// what loadCompareDiffCmd hands back: inherited from the STACK view
	arrived := diffViewWith(sameRowsTUI(10, 2), []int{2})
	arrived.inheritIdentity(dv)
	u, _ := m.Update(stackFileMsg{gen: dv.stk.gen, idx: 1, view: arrived})
	got := u.(Model).diffLayer().stk.files[1].d
	want := model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "f1.go"}
	if got.noteAddr != want || got.previewSet != set {
		t.Fatalf("file 1 landed with address %+v set %v, want %+v", got.noteAddr, got.previewSet, want)
	}
}

// A plain branch compare has no note address single-file, and a stack must
// not invent one.
func TestStackPlainCompareFilesStayUnaddressed(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(10, 2), nil)
	m := diffModel()
	m.height, m.width = 20, 120
	m.filesMode = filesModeCompare
	m = m.pushLayer(v)
	dv := m.diffLayer()
	u, _ := m.Update(stackFileMsg{gen: dv.stk.gen, idx: 1, view: diffViewWith(sameRowsTUI(10, 2), []int{2})})
	if a := u.(Model).diffLayer().stk.files[1].d.noteAddr; a.Path != "" {
		t.Fatalf("a plain compare file must stay unaddressed, got %+v", a)
	}
}

// A note box names its file and line ("agent note · Junie · f1.go R3"). In a
// stack the view that paints it is the STACK, which names no file — the title
// must come from the note's own file (address and preview set alike).
func TestStackNoteTitleNamesItsOwnFile(t *testing.T) {
	t.Parallel()
	v := stackViewOf(t, sameRowsTUI(10, 2), sameRowsTUI(10, 2))
	d := v.stk.files[1].d
	d.noteAddr = model.FileAddress{State: model.StateCommitted, Commit: "abc", Path: "f1.go"}
	d.notes = []domain.ResolvedNote{{
		Note:  model.Note{ID: "n1", Source: model.NoteSourceAgent, Author: "Junie", Side: model.NoteSideNew, Summary: "look here"},
		Range: [2]int{3, 3},
	}}
	v.rebuild()
	byLine, _ := v.noteRowIndex()
	var title string
	for _, rows := range byLine {
		for _, r := range rows {
			if r.kind == noteRowTop {
				title = r.text
			}
		}
	}
	if !strings.Contains(title, "f1.go R3") {
		t.Fatalf("the box title must name its file, got %q", title)
	}
}
