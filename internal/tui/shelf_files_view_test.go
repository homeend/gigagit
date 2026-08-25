package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// shelfFilesModel is a model with the files view open in shelf mode, populated
// with the given member paths.
func shelfFilesModel(t *testing.T, paths ...string) Model {
	t.Helper()
	m := shelfPopModel(shCommitEntry("ce"))
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	var files []model.CommitFile
	for _, p := range paths {
		files = append(files, model.CommitFile{Path: p})
	}
	mm, _ = m.Update(shelfFilesMsg{id: "ce", files: files})
	return mm.(Model)
}

func TestShelfSwitcherEnterOpensCommitFilesView(t *testing.T) {
	t.Parallel()
	m := shelfPopModel(shCommitEntry("ce"))
	mm, cmd := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.filesView == nil || m.filesMode != filesModeShelf {
		t.Fatalf("enter on a shelved commit should open the files view in shelf mode, mode=%v", m.filesMode)
	}
	if m.filesShelfID != "ce" {
		t.Fatalf("filesShelfID = %q, want ce", m.filesShelfID)
	}
	if !m.filesTreeFocused {
		t.Fatal("the tree must own focus (no live commit list behind a shelved commit)")
	}
	if m.shelfSwitcher() != nil {
		t.Fatal("the G switcher must be closed (files view is not a layer)")
	}
	if cmd == nil {
		t.Fatal("opening must dispatch the member-list load")
	}
}

func TestShelfFilesMsgPopulatesTree(t *testing.T) {
	t.Parallel()
	m := shelfFilesModel(t, "top.txt", "sub/inner.txt")
	var all []string
	for _, l := range m.filesView.lines {
		all = append(all, l.text)
	}
	joined := strings.Join(all, "\n")
	if !strings.Contains(joined, "top.txt") || !strings.Contains(joined, "inner.txt") {
		t.Fatalf("tree missing members:\n%s", joined)
	}

	// A stale message (different entry) must not clobber the view.
	mm, _ := m.Update(shelfFilesMsg{id: "other", files: []model.CommitFile{{Path: "x.go"}}})
	m = mm.(Model)
	joined = ""
	for _, l := range m.filesView.lines {
		joined += l.text + "\n"
	}
	if strings.Contains(joined, "x.go") {
		t.Fatal("a stale shelfFilesMsg must be dropped")
	}
}

func TestShelfFilesFocusedBookmarkIsShelfMember(t *testing.T) {
	t.Parallel()
	m := shelfFilesModel(t, "top.txt")
	// Select the file row (skip any heading rows).
	vis := m.filesView.visible()
	for i, l := range vis {
		if l.path == "top.txt" {
			m.filesView.sel = i
		}
	}
	b, ok := m.focusedBookmark()
	if !ok {
		t.Fatal("a shelf-mode tree row must yield a focused file")
	}
	if b.State != model.StateShelf || b.ShelfID != "ce" || b.Path != "top.txt" {
		t.Fatalf("focused = %+v, want StateShelf/ce/top.txt", b)
	}
	// The restore mechanism: the standard Copy-to-working-dir row must be offered.
	if _, ok := m.copyToWorkingDirRow(); !ok {
		t.Fatal("Copy to working dir must be offered for a shelf member")
	}
}

func TestShelfFilesEnterOpensMemberVsWorkingDiff(t *testing.T) {
	t.Parallel()
	m := shelfFilesModel(t, "top.txt")
	m.width, m.height = 100, 30
	mm, cmd := m.openDiffForFileLine(contentLine{path: "top.txt"})
	m = mm.(Model)
	if m.diffLayer() == nil {
		t.Fatal("must open the diff layer")
	}
	if m.diffTag != "shelffile:ce:top.txt" {
		t.Fatalf("diffTag = %q, want shelffile:ce:top.txt", m.diffTag)
	}
	if cmd == nil {
		t.Fatal("must dispatch the two-ref compare load")
	}
}

func TestCloseFilesViewClearsShelfCluster(t *testing.T) {
	t.Parallel()
	m := shelfFilesModel(t, "top.txt")
	m = m.closeFilesView()
	if m.filesShelfID != "" || m.filesShelfLabel != "" || m.filesMode == filesModeShelf {
		t.Fatalf("shelf cluster must be zeroed, id=%q label=%q mode=%v", m.filesShelfID, m.filesShelfLabel, m.filesMode)
	}
}

func TestShelfFilesRightColumnInert(t *testing.T) {
	t.Parallel()
	m := shelfFilesModel(t, "top.txt")
	if m2 := m.focusRight(); !m2.filesTreeFocused {
		t.Fatal("focusRight must be inert in shelf mode")
	}
	mm, _ := m.moveCommitUnderFilesView(1)
	if mm.(Model).filesMode != filesModeShelf {
		t.Fatal("commit-list movement must not replace the shelf view")
	}
}
