package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestOpenCompareFocusedVsBookmark(t *testing.T) {
	m := footerModel()
	ref := model.FileRef{Source: model.SourceCommit, Locator: "aaaa1111", Path: "a.go"}
	bm := model.Bookmark{ID: "bm9", State: model.StateCommitted, Commit: "bbbb2222", SHA: "blob22", Path: "b.go"}
	u, cmd := m.openCompareFocusedVsBookmark(ref, "commit a.go", bm)
	if u.diffView == nil {
		t.Fatal("openCompareFocusedVsBookmark must open a diff view")
	}
	if u.diffView.title != "a.go ↔ b.go" {
		t.Errorf("diff title = %q, want \"a.go ↔ b.go\"", u.diffView.title)
	}
	if u.diffTag != "cmpbm:a.go:bm9" {
		t.Errorf("diffTag = %q, want cmpbm:a.go:bm9", u.diffTag)
	}
	if cmd == nil {
		t.Error("expected a load command")
	}
}

func twoBookmarkItems() []model.Bookmark {
	return []model.Bookmark{
		{ID: "b1", State: model.StateCommitted, Commit: "c1", SHA: "s1", Path: "a.go"},
		{ID: "b2", State: model.StateCommitted, Commit: "c2", SHA: "s2", Path: "b.go"},
	}
}

func TestPendingCompareSurvivesLoad(t *testing.T) {
	m := footerModel()
	m.pendingCompare = &pendingCompare{ref: model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}, label: "commit a.go"}
	u, _ := m.Update(bookmarksLoadedMsg{items: twoBookmarkItems()})
	mm := u.(Model)
	if mm.bookmarkPopup == nil || mm.bookmarkPopup.compareRef == nil {
		t.Fatal("popup must open in compare mode")
	}
	if mm.pendingCompare != nil {
		t.Error("pendingCompare must be cleared once consumed")
	}
}

func TestCompareModeEnterRunsCompare(t *testing.T) {
	m := footerModel()
	m.bookmarkPopup = newBookmarkPopup(twoBookmarkItems())
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkPopup.compareRef = &ref
	m.bookmarkPopup.compareLabel = "commit a.go"
	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView == nil {
		t.Fatal("enter in compare mode must open the comparison diff")
	}
	if mm.bookmarkPopup != nil {
		t.Error("popup should close after launching the compare")
	}
}

func TestCompareModeMutatorsInert(t *testing.T) {
	m := footerModel()
	m.bookmarkPopup = newBookmarkPopup(twoBookmarkItems())
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkPopup.compareRef = &ref
	for _, k := range []string{"x", "p", "m"} {
		u, _ := m.Update(keyMsg(k))
		mm := u.(Model)
		if mm.bookmarkPopup == nil || mm.diffView != nil || mm.modal != nil || mm.bookmarkPastePopup != nil {
			t.Errorf("%q must be inert in compare mode", k)
		}
		if mm.bookmarkPopup != nil && mm.bookmarkPopup.markID != "" {
			t.Errorf("%q must not set a compare mark in compare mode", k)
		}
	}
}

func TestCompareRowRunSetsPendingAndLoads(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "a.go", rev: "cafe9999"} // a resolvable focused file
	row, ok := m.compareAgainstBookmarkRow()
	if !ok {
		t.Fatal("compare row must be present when a file is focused")
	}
	u, cmd := row.run(m)
	mm := u.(Model)
	if mm.pendingCompare == nil || mm.pendingCompare.ref.Path != "a.go" {
		t.Fatalf("run must set pendingCompare for the focused file, got %+v", mm.pendingCompare)
	}
	if cmd == nil {
		t.Error("run must kick off the bookmark load")
	}
}

func TestCompareRowAccompaniesAddRow(t *testing.T) {
	// Wherever "Bookmark this file" appears, so must "Compare against bookmark".
	m := footerModel()
	m = m.pushSurface(newBlameView(navContext{path: "a.go", rev: "abc123"}))
	got := ids(availableActions(m))
	if !got["bookmark-add"] {
		t.Fatal("precondition: bookmark-add expected in blame view")
	}
	if !got["bookmark-compare"] {
		t.Error("bookmark-compare must accompany bookmark-add")
	}
}
