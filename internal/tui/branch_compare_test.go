package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The Branches pair-op popup offers Compare as its 4th row, spelling out both
// names in ↔ form.
func TestPairOpsIncludeCompare(t *testing.T) {
	ops := pairOpsFor(panelBranches)
	if len(ops) != 4 {
		t.Fatalf("pairOpsFor(panelBranches) has %d ops, want 4", len(ops))
	}
	got := ops[3].label("feat/x", "main")
	if got != "Compare feat/x ↔ main" {
		t.Fatalf("compare label = %q", got)
	}
	if ops[3].open == nil || ops[3].build != nil {
		t.Fatal("compare row must use the open seam (no engine op)")
	}
}

// Enter on the Compare row opens the files view in compare mode with
// branch-name endpoints, a full-name title (Endpoint.Display would truncate
// long branch names to 7 chars), the pair state armed, popup gone, mark gone.
func TestCompareRowOpensBranchCompare(t *testing.T) {
	const marked, selected = "feature/long-branch-name", "main"
	m := Model{width: 120, height: 40}
	m.mark = &markState{panel: panelBranches, key: marked, display: marked}
	m = m.pushLayer(newPairOpPopup(m.width, marked, selected, pairOpsFor(panelBranches)))

	// Move to the 4th row (Compare) and run it.
	for range 3 {
		mm, _ := m.Update(keyMsg("j"))
		m = mm.(Model)
	}
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)

	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("compare row should open the files view in compare mode")
	}
	if m.filesLeft.Hash != marked || m.filesRight.Hash != selected {
		t.Fatalf("endpoints = %q / %q, want %q / %q", m.filesLeft.Hash, m.filesRight.Hash, marked, selected)
	}
	if !strings.Contains(m.filesTitle, marked+" ↔ "+selected) {
		t.Fatalf("title %q must carry the FULL branch names", m.filesTitle)
	}
	if m.comparePair == nil || m.comparePair.left != marked || m.comparePair.right != selected {
		t.Fatalf("comparePair = %+v, want %s/%s", m.comparePair, marked, selected)
	}
	if m.comparePair.scope != compareScopeAll {
		t.Fatalf("scope = %v, want compareScopeAll", m.comparePair.scope)
	}
	if layerOf[*pairOpPopup](m) != nil {
		t.Fatal("pair-op popup should close")
	}
	if m.mark != nil {
		t.Fatal("the mark should clear")
	}
}

// A compareOriginsMsg for the live tag lands in the pair state; a stale tag
// (view closed or a different compare opened) is dropped.
func TestCompareOriginsMsgTagGate(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")

	origins := model.CompareOrigins{APaths: map[string]bool{"a.txt": true}, BPaths: map[string]bool{}}
	mm, _ := m.Update(compareOriginsMsg{tag: m.compareTag, origins: origins})
	m = mm.(Model)
	if !m.comparePair.originsLoaded || !m.comparePair.origins.APaths["a.txt"] {
		t.Fatalf("live origins msg should land: %+v", m.comparePair)
	}

	// Stale: different tag must not clobber state.
	m.comparePair.originsLoaded = false
	mm, _ = m.Update(compareOriginsMsg{tag: "cmp:other:pair", origins: origins})
	m = mm.(Model)
	if m.comparePair.originsLoaded {
		t.Fatal("stale origins msg (tag mismatch) must be dropped")
	}
}

// The raw compare file list is retained on comparePair (Task 3 rebuilds rows
// from it when the scope changes); non-branch compares keep the old behavior.
func TestCompareFilesMsgRetainsRawListForBranchPair(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	files := []model.CommitFile{{Status: "M", Path: "a.txt"}}
	mm, _ := m.Update(compareFilesMsg{tag: m.compareTag, files: files})
	m = mm.(Model)
	if len(m.comparePair.files) != 1 || m.comparePair.files[0].Path != "a.txt" {
		t.Fatalf("raw list not retained: %+v", m.comparePair.files)
	}
}

// closeFilesView must drop the pair state (it is compare-view-scoped).
func TestCloseFilesViewClearsComparePair(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	m = m.closeFilesView()
	if m.comparePair != nil {
		t.Fatal("closeFilesView must clear comparePair")
	}
}

// Re-running the SAME branch pair keeps the showing view (the
// openCompareFiles same-tag convention) and does not re-arm state.
func TestOpenBranchCompareSamePairKeepsView(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	m.comparePair.originsLoaded = true // pretend origins landed
	m, _ = m.openBranchCompare("feat/x", "main")
	if !m.comparePair.originsLoaded {
		t.Fatal("same-pair reopen must keep the existing state (no reset)")
	}
}
