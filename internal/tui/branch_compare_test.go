package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
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

// filterCompareFiles keeps rows whose new OR old path is in the set (a
// rename matches from either side); a nil set means "all".
func TestFilterCompareFiles(t *testing.T) {
	files := []model.CommitFile{
		{Status: "M", Path: "a.txt"},
		{Status: "M", Path: "b.txt"},
		{Status: "R", Path: "r-new.txt", OldPath: "r-old.txt"},
	}
	if got := filterCompareFiles(files, nil); len(got) != 3 {
		t.Fatalf("nil set should keep all rows, got %d", len(got))
	}
	set := map[string]bool{"a.txt": true, "r-old.txt": true}
	got := filterCompareFiles(files, set)
	if len(got) != 2 || got[0].Path != "a.txt" || got[1].Path != "r-new.txt" {
		t.Fatalf("filtered = %+v, want a.txt + the rename (matched via old path)", got)
	}
}

// f cycles all -> left-only -> right-only -> all, rebuilding rows and title.
func TestFKeyCyclesScope(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	files := []model.CommitFile{
		{Status: "M", Path: "a.txt"},
		{Status: "M", Path: "b.txt"},
	}
	mm, _ := m.Update(compareFilesMsg{tag: m.compareTag, files: files})
	m = mm.(Model)
	origins := model.CompareOrigins{
		APaths: map[string]bool{"a.txt": true},
		BPaths: map[string]bool{"b.txt": true},
	}
	mm, _ = m.Update(compareOriginsMsg{tag: m.compareTag, origins: origins})
	m = mm.(Model)

	mm, _ = m.Update(keyMsg("f")) // -> left only
	m = mm.(Model)
	if m.comparePair.scope != compareScopeLeft {
		t.Fatalf("scope = %v, want left", m.comparePair.scope)
	}
	if got := len(m.filesView.lines); got != 1 {
		t.Fatalf("left-only rows = %d, want 1 (a.txt)", got)
	}
	if !strings.Contains(m.filesTitle, "only files feat/x changed") {
		t.Fatalf("title = %q", m.filesTitle)
	}

	mm, _ = m.Update(keyMsg("f")) // -> right only
	m = mm.(Model)
	if m.comparePair.scope != compareScopeRight {
		t.Fatalf("scope = %v, want right", m.comparePair.scope)
	}
	if !strings.Contains(m.filesTitle, "only files main changed") {
		t.Fatalf("title = %q", m.filesTitle)
	}

	mm, _ = m.Update(keyMsg("f")) // -> all
	m = mm.(Model)
	if m.comparePair.scope != compareScopeAll {
		t.Fatalf("scope = %v, want all", m.comparePair.scope)
	}
	if got := len(m.filesView.lines); got != 2 {
		t.Fatalf("all rows = %d, want 2", got)
	}
	if strings.Contains(m.filesTitle, "only files") {
		t.Fatalf("all-scope title should carry no filter suffix: %q", m.filesTitle)
	}
}

// f before the origin sets land: status note, scope unchanged.
func TestFKeyBeforeOriginsLoaded(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	mm, _ := m.Update(keyMsg("f"))
	m = mm.(Model)
	if m.comparePair.scope != compareScopeAll {
		t.Fatal("scope must stay all while origins are loading")
	}
	if m.statusMsg != "origin filter loading…" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

// No merge base: the typed sentinel maps to the unavailable note.
func TestFKeyNoMergeBase(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openBranchCompare("feat/x", "main")
	mm, _ := m.Update(compareOriginsMsg{tag: m.compareTag, err: fmt.Errorf("%w: exit 1", domain.ErrNoMergeBase)})
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("f"))
	m = mm.(Model)
	if m.comparePair.scope != compareScopeAll {
		t.Fatal("scope must stay all without a merge base")
	}
	if m.statusMsg != "no common ancestor — filter unavailable" {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

// f is inert in a NON-branch compare (comparePair == nil).
func TestFKeyInertInNonBranchCompare(t *testing.T) {
	m := Model{width: 120, height: 40}
	m, _ = m.openCompareFiles(
		model.Endpoint{Kind: model.EndpointCommit, Hash: "abc1234"},
		model.Endpoint{Kind: model.EndpointWorkTree})
	mm, _ := m.Update(keyMsg("f"))
	m = mm.(Model)
	if m.statusMsg != "" {
		t.Fatalf("f in a non-branch compare must be inert, got note %q", m.statusMsg)
	}
}

// The filtered view renders (guards the green-unit/broken-render class) and
// the footer hint advertises [f] filter for a branch pair.
func TestBranchCompareRendersWithFilter(t *testing.T) {
	m := loadedModel(t) // real repo + svc (nav_test.go); cmds are not invoked
	mm0, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = mm0.(Model)
	m, _ = m.openBranchCompare("feat/x", "main")
	files := []model.CommitFile{{Status: "M", Path: "a.txt"}, {Status: "M", Path: "b.txt"}}
	mm, _ := m.Update(compareFilesMsg{tag: m.compareTag, files: files})
	m = mm.(Model)
	origins := model.CompareOrigins{APaths: map[string]bool{"a.txt": true}, BPaths: map[string]bool{"b.txt": true}}
	mm, _ = m.Update(compareOriginsMsg{tag: m.compareTag, origins: origins})
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("f"))
	m = mm.(Model)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "a.txt") || strings.Contains(out, "b.txt") {
		t.Fatalf("left-only render should list a.txt and not b.txt:\n%s", out)
	}
	if !strings.Contains(out, "[f] filter") {
		t.Fatalf("footer hint missing [f] filter:\n%s", out)
	}
}
