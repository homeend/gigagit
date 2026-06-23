package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFileFinderEnterOpensActionMenu(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go"}})
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("enter"))
	m = nm.(Model)
	if m.actionMenu == nil {
		t.Fatal("enter should open the file-action menu")
	}
	got := map[string]bool{}
	for _, r := range m.actionMenu.rows {
		got[r.id] = true
	}
	for _, id := range []string{"ff-view", "ff-diff", "ff-history", "ff-blame", "ff-editor", "ff-copy-path"} {
		if !got[id] {
			t.Fatalf("missing %s; rows=%v", id, got)
		}
	}
}

func TestFileFinderHistoryActionOpensHistoryLayer(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go"}})
	m = nm.(Model)
	rows := m.fileFinderActionRows("a/b.go")
	var run func(Model) (tea.Model, tea.Cmd)
	for _, r := range rows {
		if r.id == "ff-history" {
			run = r.run
		}
	}
	nm, _ = run(m)
	m = nm.(Model)
	if layerOf[*historyView](m) == nil {
		t.Fatal("history action should push a historyView layer")
	}
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("the finder must be popped when an action opens a surface")
	}
}
