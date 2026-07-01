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
