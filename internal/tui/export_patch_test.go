package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestExportPatchPopupEnterStartsExport(t *testing.T) {
	p := &exportPatchPopup{data: []byte("From abc\n")}
	p.dest = newTextField("/tmp/repo-parent/a1b2c3d.patch")
	// Rendering must show the prefilled path and the key hints.
	m := Model{}
	m.width, m.height = 100, 30
	out := p.render(m, "")
	if !strings.Contains(out, "a1b2c3d.patch") {
		t.Fatalf("popup should show the default path:\n%s", out)
	}
	if !strings.Contains(out, "[enter]") || !strings.Contains(out, "[esc]") {
		t.Fatalf("popup should show key hints:\n%s", out)
	}
}

func TestCommitExportPatchRowHiddenForMerge(t *testing.T) {
	// A merge commit (len(Parents) > 1) must not offer the row.
	//
	// backingIndex(panelCommits) resolves through displayIndices, which walks
	// commitList{items: m.commits, m: &m}.Len() == m.commitsTotal() ==
	// wipCount()+len(m.commits). With no wip rows (m.wipRows is nil) and one
	// commit, that's index 0. m.sel is a nil map here, and reading a missing
	// key from a nil map yields the zero value (0), so the selection already
	// lands on backing index 0 without any extra setup — mirrors the same
	// zero-value reliance TestCommitShelfRowPresentOnCommits uses.
	m := Model{focus: panelCommits}
	m.commits = []model.Commit{{Hash: "deadbeef", Parents: []string{"p1", "p2"}}}
	if _, ok := m.commitExportPatchRow(); ok {
		t.Fatal("merge commit must not offer Export commit as patch")
	}
}

func TestCommitExportPatchRowPresentForNonMerge(t *testing.T) {
	// Sanity check for the positive path, alongside the merge-hidden case above.
	m := Model{focus: panelCommits}
	m.commits = []model.Commit{{Hash: "deadbeef", Parents: []string{"p1"}}}
	r, ok := m.commitExportPatchRow()
	if !ok {
		t.Fatal("non-merge commit should offer Export commit as patch")
	}
	if r.id != "commit-export-patch" || r.label != "Export commit as patch" || r.run == nil {
		t.Fatalf("bad row: %+v", r)
	}
}

// TestCommitExportPatchRowInMenu guards the action_menu.go wiring itself
// (Step 5 of the brief), not just the row helper in isolation. Mirrors
// TestCommitShelfRowInMenu (shelf_commit_test.go).
func TestCommitExportPatchRowInMenu(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	found := false
	for _, r := range availableActions(m) {
		if r.id == "commit-export-patch" {
			found = true
		}
	}
	if !found {
		t.Fatal("commit-export-patch must appear in the Commits . menu")
	}
}

func TestExportFilePatchRowOnlyForCommitDiff(t *testing.T) {
	// A commit-vs-parent file diff (rev set, not compare mode) offers the row.
	// filesModeChanged is the zero value of filesMode ("a commit's changed files
	// (vs parent)"), so this also matches a bare zero-value Model; set it
	// explicitly for clarity. (The brief calls this "filesModeCommit", which
	// does not exist in files_view.go — filesModeChanged is the real name.)
	m := Model{}
	m.filesMode = filesModeChanged
	dv := &diffView{title: "src/foo.go", rev: "abc123"}
	m = m.pushLayer(dv) // however diffView is installed; see openDiffForFileLine
	if _, ok := m.exportFilePatchRow(); !ok {
		t.Fatal("commit file diff should offer Export this file's diff as patch")
	}
	// Compare-mode diff (rev set but comparing two endpoints) must NOT offer it.
	m2 := Model{}
	m2.filesMode = filesModeCompare
	dv2 := &diffView{title: "src/foo.go", rev: "abc123"}
	m2 = m2.pushLayer(dv2)
	if _, ok := m2.exportFilePatchRow(); ok {
		t.Fatal("compare-mode diff must not offer file patch export")
	}
	// A working-tree diff (rev == "") must NOT offer it.
	m3 := Model{}
	dv3 := &diffView{title: "src/foo.go", rev: ""}
	m3 = m3.pushLayer(dv3)
	if _, ok := m3.exportFilePatchRow(); ok {
		t.Fatal("working-tree diff must not offer file patch export")
	}
}

// TestExportFilePatchRowHiddenBehindHistorySurface guards against a leak found
// during review: diff_view.go's "h"/"b" keys push a *historyView/*blameView ON
// TOP of the diff view without popping it (so the split-pane right side can
// reuse the diff). m.diffLayer() (layerOf[*diffView]) scans the WHOLE stack
// top-down and would still find the buried diff, but the diff is no longer the
// front surface — the same distinction availableActions's onStackFile check
// makes for the neighboring files-view rows. exportFilePatchRow must key off
// the literal top of the stack (m.topLayer()), not "a diff exists somewhere."
func TestExportFilePatchRowHiddenBehindHistorySurface(t *testing.T) {
	m := Model{}
	dv := &diffView{title: "src/foo.go", rev: "abc123"}
	m = m.pushLayer(dv)
	hv := newHistoryView(navContext{path: "src/foo.go", rev: "abc123"})
	m = m.pushLayer(hv)
	if _, ok := m.exportFilePatchRow(); ok {
		t.Fatal("history surface on top of a diff must not offer file patch export for the buried diff")
	}
}

// TestExportFilePatchRowInMenu guards the action_menu.go wiring itself (the
// row must actually surface through availableActions when a commit-vs-parent
// diff is front), not just the row helper in isolation. Mirrors
// TestCommitExportPatchRowInMenu.
func TestExportFilePatchRowInMenu(t *testing.T) {
	m := Model{}
	m.filesMode = filesModeChanged
	dv := &diffView{title: "src/foo.go", rev: "abc123"}
	m = m.pushLayer(dv)
	found := false
	for _, r := range availableActions(m) {
		if r.id == "file-export-patch" {
			found = true
		}
	}
	if !found {
		t.Fatal("file-export-patch must appear in the diff view . menu")
	}
}
