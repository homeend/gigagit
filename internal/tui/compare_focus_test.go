package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// openCompareFiles must focus the file tree: in compare mode there is no live
// commit list, and moving the commit selection would discard the comparison.
func TestCompareOpensTreeFocused(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	mm, _ := m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash},
		model.Endpoint{Kind: model.EndpointWorkTree})
	if !mm.filesTreeFocused {
		t.Fatal("compare files view must open with the tree focused")
	}
}

// In compare mode, up/down must not move the commit selection / discard the
// comparison — it navigates the file tree instead.
func TestCompareModeMoveKeepsComparison(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m, _ = m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash},
		model.Endpoint{Kind: model.EndpointWorkTree})
	m.filesView.lines = []contentLine{
		{text: "M a.go", path: "a.go", status: "M"},
		{text: "M b.go", path: "b.go", status: "M"},
	}
	tagBefore := m.compareTag
	hashBefore := m.filesHash
	selBefore := m.sel[panelCommits]

	u, _ := m.Update(keyMsg("down"))
	mm := u.(Model)

	if !mm.filesCompare || mm.compareTag != tagBefore || mm.filesHash != hashBefore {
		t.Fatalf("down discarded the comparison: compare=%v tag=%q hash=%q",
			mm.filesCompare, mm.compareTag, mm.filesHash)
	}
	if mm.sel[panelCommits] != selBefore {
		t.Fatalf("down moved the commit selection (%d→%d) in compare mode", selBefore, mm.sel[panelCommits])
	}
	if mm.filesView.sel != 1 {
		t.Fatalf("down should move the tree selection to 1, got %d", mm.filesView.sel)
	}
}

// The mouse path (wheel/click over the commits region) calls
// moveCommitUnderFilesView directly, bypassing the keyboard wrapper — it must
// also be locked out in compare mode, or it discards the comparison.
func TestCompareModeMouseScrollKeepsComparison(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m, _ = m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: m.commits[1].Hash},
		model.Endpoint{Kind: model.EndpointWorkTree})
	tagBefore, hashBefore, selBefore := m.compareTag, m.filesHash, m.sel[panelCommits]

	u, _ := m.moveCommitUnderFilesView(1) // the mouse path
	mm := u.(Model)

	if !mm.filesCompare || mm.compareTag != tagBefore || mm.filesHash != hashBefore || mm.sel[panelCommits] != selBefore {
		t.Fatalf("mouse-path move discarded the comparison: compare=%v tag=%q hash=%q sel=%d",
			mm.filesCompare, mm.compareTag, mm.filesHash, mm.sel[panelCommits])
	}
}

// Marking two commit rows opens their whole-tree compare directly (older→newer).
// Marking two commits adds both to the ◉ selection set (no auto-diff); the
// `.`-menu "Compare selection" then opens the whole-tree diff, ordered
// older→newer.
func TestMarkTwoCommitsSelectThenCompare(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // commits[0]=tip (newer), commits[1] older
	m.focus = panelCommits

	m.sel[panelCommits] = 0
	u, _ := m.Update(keyMsg("m")) // select the tip
	m = u.(Model)
	m.sel[panelCommits] = 1
	u, _ = m.Update(keyMsg("m")) // select an older commit
	m = u.(Model)

	if m.filesView != nil {
		t.Fatal("marking commits must not auto-open a compare view")
	}
	if !m.commitCompareSet[m.commits[0].Hash] || !m.commitCompareSet[m.commits[1].Hash] {
		t.Fatalf("both commits must be in the selection set, got %v", m.commitCompareSet)
	}

	r, ok := m.commitCompareSelectionRow()
	if !ok {
		t.Fatal("compare-selection row must be present with 2 commits")
	}
	u, _ = r.run(m)
	mm := u.(Model)
	if mm.filesView == nil || !mm.filesCompare {
		t.Fatal("Compare selection must open the compare files view")
	}
	// older (commits[1]) → newer (commits[0]).
	if mm.filesLeft.Hash != m.commits[1].Hash || mm.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s↔%s, want older↔newer", mm.filesLeft.Hash, mm.filesRight.Hash)
	}
}

// Selecting a commit and the Working tree row, then "Compare selection",
// compares the commit against the working tree (the case the user hit "no pair
// operations" on).
func TestSelectCommitThenWorktreeCompares(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Select an old commit (unified index = wipCount + 1).
	m.sel[panelCommits] = m.wipCount() + 1
	u, _ := m.Update(keyMsg("m"))
	m = u.(Model)
	// Select the Working tree row (unified index 0).
	m.sel[panelCommits] = 0
	u, _ = m.Update(keyMsg("m"))
	m = u.(Model)

	r, ok := m.commitCompareSelectionRow()
	if !ok {
		t.Fatal("compare-selection row must be present with a commit + wip row")
	}
	u, _ = r.run(m)
	mm := u.(Model)
	if mm.filesView == nil || !mm.filesCompare {
		t.Fatal("Compare selection must open a compare")
	}
	// commit (older) → working tree (newer).
	if mm.filesLeft.Kind != model.EndpointCommit || mm.filesRight.Kind != model.EndpointWorkTree {
		t.Fatalf("endpoints = %v↔%v, want Commit↔WorkTree", mm.filesLeft.Kind, mm.filesRight.Kind)
	}
}
