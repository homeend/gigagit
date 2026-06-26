package tui

import "testing"

// footerModel() has branches: main (IsHead, row 0), feat/x (row 1); status.Branch=main.

func TestPushBranchRowOnNonCurrentBranch(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	m.sel = map[panel]int{panelBranches: 1} // feat/x, not the checked-out branch
	r, ok := m.pushBranchRow()
	if !ok {
		t.Fatal("push-branch row must be present on a non-current branch")
	}
	if r.run == nil {
		t.Fatal("push-branch row must have a run handler")
	}
	// The label embeds the SELECTED branch, not the HEAD branch — this is the bug:
	// before the fix, pushing acted on m.status.Branch (main), never feat/x.
	if want := "Push feat/x"; r.label != want {
		t.Fatalf("label = %q, want %q", r.label, want)
	}
}

func TestPushBranchRowOnCurrentBranch(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches // row 0 = main (IsHead)
	r, ok := m.pushBranchRow()
	if !ok {
		t.Fatal("push-branch row must be present on the current branch too")
	}
	if want := "Push main"; r.label != want {
		t.Fatalf("label = %q, want %q", r.label, want)
	}
}

func TestPushBranchRowRuns(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	m.sel = map[panel]int{panelBranches: 1} // feat/x
	r, ok := m.pushBranchRow()
	if !ok {
		t.Fatal("push-branch row must be present")
	}
	tm, cmd := r.run(m)
	if cmd == nil {
		t.Fatal("running push-branch must start an op (non-nil cmd)")
	}
	if !tm.(Model).running {
		t.Fatal("running push-branch must mark the model running")
	}
}

func TestPushBranchRowHiddenOffBranchesPanel(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	if _, ok := m.pushBranchRow(); ok {
		t.Fatal("push-branch must be a Branches-panel action only")
	}
}

func TestPushBranchRowWiredIntoMenu(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	m.sel = map[panel]int{panelBranches: 1} // feat/x
	if _, ok := findRow(availableActions(m), "push-branch"); !ok {
		t.Fatal("availableActions missing push-branch on the Branches panel")
	}
}
