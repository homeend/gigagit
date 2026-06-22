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

// Marking two commit rows opens their whole-tree compare directly (older→newer).
func TestMarkTwoCommitsOpensCompare(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // commits[0]=tip (newer), commits[1] older
	m.focus = panelCommits

	m.sel[panelCommits] = 0
	u, _ := m.Update(keyMsg("m")) // mark the tip
	m = u.(Model)
	m.sel[panelCommits] = 1
	u, _ = m.Update(keyMsg("m")) // mark an older commit → compare
	mm := u.(Model)

	if mm.filesView == nil || !mm.filesCompare {
		t.Fatal("marking two commits must open the compare files view")
	}
	if mm.mark != nil {
		t.Fatal("the pair action must clear the mark")
	}
	// older (commits[1]) → newer (commits[0]).
	if mm.filesLeft.Hash != m.commits[1].Hash || mm.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s↔%s, want older↔newer", mm.filesLeft.Hash, mm.filesRight.Hash)
	}
}

// Marking a commit then the Working tree row compares the commit against the
// working tree (the case the user hit "no pair operations" on).
func TestMarkCommitThenWorktreeCompares(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Mark an old commit (unified index = wipCount + 1).
	m.sel[panelCommits] = m.wipCount() + 1
	u, _ := m.Update(keyMsg("m"))
	m = u.(Model)
	// Move to the Working tree row (unified index 0) and mark → compare.
	m.sel[panelCommits] = 0
	u, _ = m.Update(keyMsg("m"))
	mm := u.(Model)

	if mm.filesView == nil || !mm.filesCompare {
		t.Fatal("marking a commit then the working-tree row must open a compare")
	}
	// commit (older) → working tree (newer).
	if mm.filesLeft.Kind != model.EndpointCommit || mm.filesRight.Kind != model.EndpointWorkTree {
		t.Fatalf("endpoints = %v↔%v, want Commit↔WorkTree", mm.filesLeft.Kind, mm.filesRight.Kind)
	}
}
