package tui

import "testing"

func TestForcePushRowOnCurrentBranch(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches // selection defaults to row 0 = "main" (IsHead)
	r, ok := findRow(availableActions(m), "force-push")
	if !ok {
		t.Fatal("availableActions missing force-push on the current branch")
	}
	if r.label != "Force push main" {
		t.Fatalf("label = %q, want 'Force push main'", r.label)
	}
}

func TestForcePushRowHiddenOnNonCurrentBranch(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	m.sel = map[panel]int{panelBranches: 1} // row 1 = "feat/x" (not IsHead)
	if _, ok := findRow(availableActions(m), "force-push"); ok {
		t.Fatal("force-push must be hidden on a non-current branch")
	}
}

func TestForcePushRowHiddenOffBranchesPanel(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	if _, ok := findRow(availableActions(m), "force-push"); ok {
		t.Fatal("force-push must be a Branches-panel action only")
	}
}
