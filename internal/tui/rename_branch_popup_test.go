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
	if !ok || m.renameBranchPopup == nil {
		t.Fatalf("popup did not open")
	}
	if m.renameBranchPopup.old != "old" || m.renameBranchPopup.name != "old" {
		t.Fatalf("want prefilled current name, got %+v", m.renameBranchPopup)
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
	res, _ := m.updateRenameBranchPopupKey(tea.KeyMsg{Type: tea.KeyEsc})
	if res.(Model).renameBranchPopup != nil {
		t.Fatalf("esc should close the popup")
	}
}
