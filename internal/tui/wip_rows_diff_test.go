package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestWipSingleSelectOpensCompare(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Select the Staged row (unified index 1) and press l.
	m.sel[panelCommits] = 1
	u, _ := m.Update(keyMsg("l"))
	mm := u.(Model)
	if mm.filesView == nil {
		t.Fatal("l on a wip row must open the compare files view")
	}
	if mm.filesLeft.Kind != model.EndpointCommit || mm.filesLeft.Hash != m.commits[0].Hash {
		t.Fatalf("staged row left endpoint = %+v, want HEAD %s", mm.filesLeft, m.commits[0].Hash)
	}
	if mm.filesRight.Kind != model.EndpointIndex {
		t.Fatalf("staged row right endpoint = %v, want EndpointIndex", mm.filesRight.Kind)
	}
}

func TestWipWorktreeRowComparesAgainstIndex(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus() // both rows present
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	m.sel[panelCommits] = 0 // Working tree row
	u, _ := m.Update(keyMsg("l"))
	mm := u.(Model)
	if mm.filesView == nil {
		t.Fatal("l on the working-tree row must open the compare files view")
	}
	if mm.filesLeft.Kind != model.EndpointIndex || mm.filesRight.Kind != model.EndpointWorkTree {
		t.Fatalf("working-tree row endpoints = %v↔%v, want Index↔WorkTree", mm.filesLeft.Kind, mm.filesRight.Kind)
	}
}

func TestWipEnterOpensCompare(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	m.sel[panelCommits] = 1 // Staged row
	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.filesView == nil {
		t.Fatal("enter on a wip row must open the compare files view")
	}
	if mm.filesLeft.Kind != model.EndpointCommit || mm.filesLeft.Hash != m.commits[0].Hash {
		t.Fatalf("staged row left endpoint = %+v, want HEAD %s", mm.filesLeft, m.commits[0].Hash)
	}
	if mm.filesRight.Kind != model.EndpointIndex {
		t.Fatalf("staged row right endpoint = %v, want EndpointIndex", mm.filesRight.Kind)
	}
}

// TestJumpToCommitUnderDirtyTree guards the write-side offset: a hash→row jump
// (tag-jump / go-to-tip) walks displayIndices, which yields UNIFIED indices.
// Reading the commit must go through commitAtUnified (offset by wipCount), and
// the resulting display position must round-trip back through backingIndex to the
// same commit. A raw m.commits[ci] lookup would land on the wrong row when dirty.
func TestJumpToCommitUnderDirtyTree(t *testing.T) {
	m := loadedModelLinearCommits(t, 4)
	m.focus = panelCommits
	m.status = dirtyStatus() // 2 wip rows
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()
	target := m.commits[2].Hash

	idx := m.displayIndices(panelCommits)
	found := -1
	for di, ci := range idx {
		if c, ok := m.commitAtUnified(ci); ok && c.Hash == target {
			found = di
		}
	}
	if found < 0 {
		t.Fatal("target commit not found via commitAtUnified")
	}
	if found != 2+m.wipCount() {
		t.Fatalf("display pos = %d, want %d (commit row offset by wipCount)", found, 2+m.wipCount())
	}
	m.sel[panelCommits] = found
	bi, ok := m.backingIndex(panelCommits)
	if !ok || m.commits[bi].Hash != target {
		t.Fatalf("jump did not round-trip: bi=%d ok=%v hash=%q want %q", bi, ok, m.commits[bi].Hash, target)
	}
}

func TestWipRowRefusesCommitOps(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "u", Unstaged: 'M'}}}
	m.wipRows = deriveWipRows(m.status)
	m.sel[panelCommits] = 0 // the Working tree wip row

	if _, ok := m.commitDropRow(); ok {
		t.Fatal("Drop commit must be unavailable on a wip row")
	}
	if _, ok := m.commitCherryPickRow(); ok {
		t.Fatal("Cherry-pick must be unavailable on a wip row")
	}
	if _, ok := m.commitRevertRow(); ok {
		t.Fatal("Revert must be unavailable on a wip row")
	}
}
