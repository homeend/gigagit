package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

func TestFileFinderEnterOpensActionMenu(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{"a/b.go"}})
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("enter"))
	m = nm.(Model)
	if m.actionMenu == nil {
		t.Fatal("enter should open the file-action menu")
	}
	got := map[string]bool{}
	for _, r := range m.actionMenu.rows {
		got[r.id] = true
	}
	for _, id := range []string{"ff-view", "ff-diff", "ff-history", "ff-blame", "ff-editor", "ff-copy-path", "ff-copy-abspath", "ff-commits-touching"} {
		if !got[id] {
			t.Fatalf("missing %s; rows=%v", id, got)
		}
	}
}

// finderRow is a small helper that returns the run func for the given row id
// from fileFinderActionRows, or fails the test if the id is absent.
func finderRow(t *testing.T, rows []actionRow, id string) func(Model) (tea.Model, tea.Cmd) {
	t.Helper()
	for _, r := range rows {
		if r.id == id {
			return r.run
		}
	}
	t.Fatalf("fileFinderActionRows missing row %q", id)
	return nil
}

// finderSetup opens the file finder in a 2-commit model, delivers the
// lsFilesMsg for path, and returns the model + action rows for path.
func finderSetup(t *testing.T, path string) (Model, []actionRow) {
	t.Helper()
	m := loadedModelLinearCommits(t, 2)
	m, _ = m.openFileFinder()
	nm, _ := m.Update(lsFilesMsg{paths: []string{path}})
	m = nm.(Model)
	return m, m.fileFinderActionRows(path)
}

func TestFileFinderHistoryActionOpensHistoryLayer(t *testing.T) {
	m, rows := finderSetup(t, "a/b.go")
	nm, _ := finderRow(t, rows, "ff-history")(m)
	m = nm.(Model)
	if layerOf[*historyView](m) == nil {
		t.Fatal("history action should push a historyView layer")
	}
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("the finder must be popped when an action opens a surface")
	}
}

func TestFileFinderDiffActionOpensDiffLayer(t *testing.T) {
	const path = "a/b.go"
	m, rows := finderSetup(t, path)
	nm, _ := finderRow(t, rows, "ff-diff")(m)
	m = nm.(Model)

	if layerOf[*diffView](m) == nil {
		t.Fatal("ff-diff should push a diffView layer")
	}
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("the finder must be popped when the diff action runs")
	}

	// Guard the tag coupling: ff-diff inlines the tag; loadCompareDiffCmd also
	// builds it from the same formula. Assert they byte-match so a future drift
	// in either side fails this test rather than causing a silent hang.
	left := model.Endpoint{Kind: model.EndpointCommit, Hash: "HEAD"}
	right := model.Endpoint{Kind: model.EndpointWorkTree}
	wantTag := "cmp:" + left.CacheTag() + ":" + right.CacheTag() + ":" + path
	if m.diffTag == "" {
		t.Fatal("ff-diff should set m.diffTag")
	}
	if m.diffTag != wantTag {
		t.Fatalf("diffTag mismatch\n got:  %q\nwant: %q", m.diffTag, wantTag)
	}
}

func TestFileFinderBlameActionOpensBlameLayer(t *testing.T) {
	m, rows := finderSetup(t, "a/b.go")
	nm, _ := finderRow(t, rows, "ff-blame")(m)
	m = nm.(Model)
	if layerOf[*blameView](m) == nil {
		t.Fatal("ff-blame should push a blameView layer")
	}
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("the finder must be popped when the blame action runs")
	}
}

func TestFileFinderViewActionOpensContentLayer(t *testing.T) {
	m, rows := finderSetup(t, "a/b.go")
	nm, _ := finderRow(t, rows, "ff-view")(m)
	m = nm.(Model)
	if layerOf[*contentPopup](m) == nil {
		t.Fatal("ff-view should push a contentPopup layer")
	}
	if layerOf[*fileFinderPopup](m) != nil {
		t.Fatal("the finder must be popped when the view action runs")
	}
}

func TestFileFinderCommitsTouchingSeedsPathFilter(t *testing.T) {
	m := loadedModel(t)
	rows := m.fileFinderActionRows("internal/engine/ops_basic.go")
	var run func(Model) (tea.Model, tea.Cmd)
	for _, r := range rows {
		if r.id == "ff-commits-touching" {
			run = r.run
		}
	}
	if run == nil {
		t.Fatal("fuzzy finder missing 'Commits touching this' row")
	}
	mm, _ := run(m)
	got := mm.(Model)
	if len(got.commitFilter.Paths) != 1 || got.commitFilter.Paths[0] != "internal/engine/ops_basic.go" {
		t.Fatalf("path not seeded: %+v", got.commitFilter.Paths)
	}
	if got.commitFilter.Author != "" || got.commitFilter.Grep != "" {
		t.Fatal("seeding a path should clear the other axes")
	}
	if got.focus != panelCommits {
		t.Fatal("should focus Commits after seeding")
	}
}

func TestFileFinderEditorAndCopyReturnCmds(t *testing.T) {
	m, rows := finderSetup(t, "a/b.go")

	editorRun := finderRow(t, rows, "ff-editor")
	nm, cmd := editorRun(m)
	m2 := nm.(Model)
	if cmd == nil {
		t.Fatal("ff-editor should return a non-nil tea.Cmd")
	}
	if layerOf[*fileFinderPopup](m2) != nil {
		t.Fatal("the finder must be popped by ff-editor")
	}

	copyRun := finderRow(t, rows, "ff-copy-path")
	nm, cmd = copyRun(m)
	m3 := nm.(Model)
	if cmd == nil {
		t.Fatal("ff-copy-path should return a non-nil tea.Cmd")
	}
	if layerOf[*fileFinderPopup](m3) != nil {
		t.Fatal("the finder must be popped by ff-copy-path")
	}
}

func TestFileFinderCopyAbsPathRow(t *testing.T) {
	m, rows := finderSetup(t, "a/b.go")
	run := finderRow(t, rows, "ff-copy-abspath")
	nm, cmd := run(m)
	if cmd == nil {
		t.Fatal("ff-copy-abspath should return a non-nil tea.Cmd")
	}
	if layerOf[*fileFinderPopup](nm.(Model)) != nil {
		t.Fatal("the finder must be popped by ff-copy-abspath")
	}
}
