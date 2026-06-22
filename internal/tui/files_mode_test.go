package tui

import "testing"

// The new mode must agree with the legacy booleans at each open path.
func TestFilesModeMatchesLegacyBooleans(t *testing.T) {
	// changed-files open (l-key path sets these)
	m := loadedModelLinearCommits(t, 2)
	m.filesView = &contentPopup{}
	m.filesAllFiles = false
	m.filesCompare = false
	m.filesMode = filesModeChanged
	if m.inCompareMode() != m.filesCompare || m.inFullTree() != m.filesAllFiles {
		t.Fatal("changed: helpers must equal legacy booleans")
	}
	// compare open
	m.filesCompare = true
	m.filesMode = filesModeCompare
	if !m.inCompareMode() || m.inFullTree() {
		t.Fatal("compare: inCompareMode true, inFullTree false")
	}
	// full-tree
	m.filesCompare = false
	m.filesAllFiles = true
	m.filesMode = filesModeFullTree
	if m.inCompareMode() || !m.inFullTree() {
		t.Fatal("fullTree: inFullTree true, inCompareMode false")
	}
}
