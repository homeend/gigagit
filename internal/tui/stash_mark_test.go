package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func statusModel() Model {
	m := Model{width: 100, height: 30, focus: panelFiles, sel: map[panel]int{}}
	m.status = model.WorkingTreeStatus{Branch: "main", Files: []model.FileStatus{
		{Path: "a.go", Unstaged: 'M'},
		{Path: "b.go", Unstaged: 'M'},
	}}
	return m
}

func TestStatusMMultiMarks(t *testing.T) {
	m := statusModel()
	m.sel[panelFiles] = 0
	mm, _ := m.handleMarkKey()
	m = mm.(Model)
	m.sel[panelFiles] = 1
	mm, _ = m.handleMarkKey()
	m = mm.(Model)
	if !m.fileMarks["a.go"] || !m.fileMarks["b.go"] {
		t.Fatalf("both files should be marked, got %v", m.fileMarks)
	}
	m.sel[panelFiles] = 0
	mm, _ = m.handleMarkKey()
	m = mm.(Model)
	if m.fileMarks["a.go"] || !m.fileMarks["b.go"] {
		t.Fatalf("a.go should toggle off, b.go stay, got %v", m.fileMarks)
	}
}

func TestBranchesMUnchanged(t *testing.T) {
	m := Model{width: 100, height: 30, focus: panelBranches, sel: map[panel]int{}}
	m.branches = []model.Branch{{Name: "main"}, {Name: "feat"}}
	m.sel[panelBranches] = 0
	mm, _ := m.handleMarkKey()
	if mm.(Model).mark == nil {
		t.Fatal("Branches m must still set the single mark")
	}
}
