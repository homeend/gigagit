package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
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
	if mm.filesLeft.Kind != model.EndpointIndex {
		t.Fatalf("staged row left endpoint = %v, want EndpointIndex", mm.filesLeft.Kind)
	}
	if mm.filesRight.Kind != model.EndpointCommit || mm.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("staged row right endpoint = %+v, want HEAD %s", mm.filesRight, m.commits[0].Hash)
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
	if mm.filesLeft.Kind != model.EndpointWorkTree || mm.filesRight.Kind != model.EndpointIndex {
		t.Fatalf("working-tree row endpoints = %v↔%v, want WorkTree↔Index", mm.filesLeft.Kind, mm.filesRight.Kind)
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
	if mm.filesLeft.Kind != model.EndpointIndex {
		t.Fatalf("staged row left endpoint = %v, want EndpointIndex", mm.filesLeft.Kind)
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
