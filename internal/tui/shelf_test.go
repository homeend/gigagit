package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

func TestAddToShelfRowOnFilesPanel(t *testing.T) {
	m := filesMenuModel() // panelFiles focused with one tracked file
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "shelf-add"); !ok {
		t.Fatalf("Add to shelf missing from menu on Files panel")
	}
	a, ok := m.focusedShelfAddress()
	if !ok || a.State != model.StateUnstaged || a.Path != "dir/f.txt" || a.Worktree != "/wt" {
		t.Fatalf("focusedShelfAddress = %+v ok=%v, want unstaged dir/f.txt @ /wt", a, ok)
	}
}

func TestShelfAddCaptureFromBlame(t *testing.T) {
	// Working-tree blame (ctx.rev == "") captures the current worktree's working
	// file — the worktree/branch come from the Model (derived ad-hoc), not stored
	// on the view. Mirrors the working-tree diff-view capture.
	m := footerModel().pushLayer(blameFixture()) // ctx.rev == ""
	m.currentWorktree = "/wt"
	m.status.Branch = "main"
	a, ok := m.focusedShelfAddress()
	if !ok || a.State != model.StateUnstaged || a.Worktree != "/wt" || a.Path != "a.go" {
		t.Fatalf("working-tree blame capture = %+v ok=%v, want unstaged a.go @ /wt", a, ok)
	}
	// A committed blame captures the commit.
	m2 := footerModel().pushLayer(&blameView{ctx: navContext{path: "a.go", rev: "abc1234def"}})
	c, ok := m2.focusedShelfAddress()
	if !ok || c.State != model.StateCommitted || c.Commit != "abc1234def" || c.Path != "a.go" {
		t.Fatalf("committed blame capture = %+v ok=%v", c, ok)
	}
}

func TestAddToShelfRowAbsentWhenNoFileFocused(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.focusedShelfAddress(); ok {
		t.Fatalf("no file focused on Branches panel; focusedShelfAddress should be false")
	}
	if _, ok := findRow(availableActions(m), "shelf-add"); ok {
		t.Fatalf("Add to shelf should not appear with no file focused")
	}
}

func TestShelfRestorePopupRequiresDest(t *testing.T) {
	m := footerModel()
	m = m.pushLayer(&shelfRestorePopup{entryID: "unstaged-a-go-deadbeef", origin: "a.go"})
	// Enter with an empty dest is a no-op (popup stays open).
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if shelfRestoreOf(m) == nil {
		t.Fatalf("empty dest should keep the popup open")
	}
	// Typing builds the destination.
	for _, r := range "out.txt" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	if shelfRestoreOf(m).dest.Value() != "out.txt" {
		t.Fatalf("dest = %q, want out.txt", shelfRestoreOf(m).dest.Value())
	}
}
