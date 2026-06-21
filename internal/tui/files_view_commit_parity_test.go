package tui

import "testing"

// filesViewGraphModel builds a graph-active Model (1 commit, focus panelCommits)
// with the commit files view open over it. treeFocused picks the side: false is
// the commit-list side (where parity with the Commits panel is expected), true
// is the file-tree side.
func filesViewGraphModel(treeFocused bool) Model {
	m := graphWinModel(50, 40, 8, 0)
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTitle = "Files h0 subject"
	m.filesHash = "h0"
	m.filesTreeFocused = treeFocused
	return m
}

// On the commit-list side the graph window keys must behave exactly as they do
// in the bare Commits panel: > widens, shift+arrows pan, = snaps.
func TestFilesViewCommitSideDrivesGraphWindow(t *testing.T) {
	m := filesViewGraphModel(false)
	m.cfg.UI.CommitGraphStep = 4
	m.cfg.UI.CommitGraphPanStep = 5

	m = feedKey(m, ">")
	if m.graphCols() != 12 {
		t.Fatalf("cols = %d, want 12 after widen on the commit side", m.graphCols())
	}
	m = feedKey(m, "shift+right")
	if m.commitGraphScroll != 5 {
		t.Fatalf("scroll = %d, want 5 after pan on the commit side", m.commitGraphScroll)
	}
	m = feedKey(m, "shift+left")
	if m.commitGraphScroll != 0 {
		t.Fatalf("scroll = %d, want 0 after pan-back", m.commitGraphScroll)
	}
}

// On the file-tree side the same keys must stay with the tree (horizontal
// scroll), never touching the graph window.
func TestFilesViewTreeSideKeepsTreeHscroll(t *testing.T) {
	m := filesViewGraphModel(true)
	m.filesView.mode = modeScroll

	m = feedKey(m, "shift+right")
	if m.commitGraphScroll != 0 {
		t.Fatalf("graph scroll = %d, want unchanged (0) on the tree side", m.commitGraphScroll)
	}
	if m.filesView.hscroll == 0 {
		t.Fatal("tree hscroll did not advance on the tree side")
	}

	before := m.graphCols()
	m = feedKey(m, ">")
	if m.graphCols() != before {
		t.Fatalf("graph cols changed (%d→%d) on the tree side", before, m.graphCols())
	}
}

// On the commit-list side the . menu must offer the full commit operations
// (parity with the Commits panel) plus the graph window controls.
func TestFilesViewCommitSideMenuHasCommitOps(t *testing.T) {
	m := filesViewGraphModel(false)
	got := map[string]bool{}
	for _, r := range availableActions(m) {
		got[r.id] = true
	}
	for _, id := range []string{"commit-cherry-pick", "commit-revert", "commit-reset", "graph-widen"} {
		if !got[id] {
			t.Errorf("commit-side menu missing %q (expected Commits-panel parity)", id)
		}
	}
}

// Every row offered on the commit-list side must carry a run handler. A row
// without one is a key-replay panel binding that the files view would swallow
// (or, for [l], close the view) — exactly the leak the copy-only early-return
// guards against. This invariant fails fast if such a row ever leaks back in.
func TestFilesViewCommitSideMenuRowsAllRunnable(t *testing.T) {
	m := filesViewGraphModel(false)
	for _, r := range availableActions(m) {
		if r.run == nil {
			t.Errorf("commit-side menu row id=%q key=%q has no run handler (would be swallowed)", r.id, r.key)
		}
	}
}

// On the file-tree side the . menu stays copy-only (file context), with no
// commit operations.
func TestFilesViewTreeSideMenuIsCopyOnly(t *testing.T) {
	m := filesViewGraphModel(true)
	for _, r := range availableActions(m) {
		if r.id == "commit-cherry-pick" {
			t.Fatal("tree-side menu must not offer commit operations")
		}
	}
}

// A listed commit-op row must actually RUN with the files view open — its run
// handler executes directly (no key-replay that the files view would swallow).
// "Create branch here" is a pure action: it pushes the branch popup.
func TestFilesViewCommitSideMenuRowRuns(t *testing.T) {
	m := filesViewGraphModel(false)
	var row actionRow
	for _, r := range availableActions(m) {
		if r.id == "commit-create-branch" {
			row = r
		}
	}
	if row.run == nil {
		t.Fatal("commit-create-branch row not offered (or has no run handler)")
	}
	mm, _ := row.run(m)
	if _, ok := mm.(Model).topLayer().(*branchPopup); !ok {
		t.Fatalf("running the row did not open the branch popup over the files view; top = %T", mm.(Model).topLayer())
	}
}
