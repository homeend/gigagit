package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

func TestRenameBranchPopupOpensPrefilled(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	m.branches = []model.Branch{{Name: "old"}}
	m, ok := m.openRenameBranchPopup()
	rp := layerOf[*renameBranchPopup](m)
	if !ok || rp == nil {
		t.Fatalf("popup did not open")
	}
	if rp.old != "old" || rp.name != "old" {
		t.Fatalf("want prefilled current name, got %+v", rp)
	}
}

func TestRenameBranchMenuRowPresent(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if got := ids(availableActions(m)); !got["rename-branch"] {
		t.Fatalf("Branches panel menu should offer rename-branch, got %v", got)
	}
	m.focus = panelWorktrees
	if got := ids(availableActions(m)); got["rename-branch"] {
		t.Fatalf("rename-branch must not appear off the Branches panel, got %v", got)
	}
}

func TestRenameBranchPopupEscCancels(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	m.branches = []model.Branch{{Name: "old"}}
	m, _ = m.openRenameBranchPopup()
	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if layerOf[*renameBranchPopup](res.(Model)) != nil {
		t.Fatalf("esc should close the popup")
	}
}
