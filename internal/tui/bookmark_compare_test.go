package tui

import (
	"errors"
	"strings"
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
	if mm.bookmarkSwitcher() == nil || mm.bookmarkSwitcher().compareRef == nil {
		t.Fatal("popup must open in compare mode")
	}
	if mm.pendingCompare != nil {
		t.Error("pendingCompare must be cleared once consumed")
	}
}

func TestCompareModeEnterRunsCompare(t *testing.T) {
	m := footerModel()
	m = m.pushOverlay(newBookmarkPopup(twoBookmarkItems()))
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkSwitcher().compareRef = &ref
	m.bookmarkSwitcher().compareLabel = "commit a.go"
	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.diffView == nil {
		t.Fatal("enter in compare mode must open the comparison diff")
	}
	if mm.bookmarkSwitcher() != nil {
		t.Error("popup should close after launching the compare")
	}
}

func TestCompareModeMutatorsInert(t *testing.T) {
	m := footerModel()
	m = m.pushOverlay(newBookmarkPopup(twoBookmarkItems()))
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkSwitcher().compareRef = &ref
	for _, k := range []string{"x", "p", "m"} {
		u, _ := m.Update(keyMsg(k))
		mm := u.(Model)
		if mm.bookmarkSwitcher() == nil || mm.diffView != nil || mm.modal != nil || bookmarkPasteOf(mm) != nil {
			t.Errorf("%q must be inert in compare mode", k)
		}
		if mm.bookmarkSwitcher() != nil && mm.bookmarkSwitcher().markID != "" {
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

// Launched from a history/blame surface, the compare diff must paint over the
// surface stack — render checks stackTop before diffView, so the popup→diff
// handoff has to clear the stack.
func TestCompareDiffVisibleOverHistorySurface(t *testing.T) {
	m := footerModel()
	m = m.pushSurface(newHistoryView(navContext{path: "a.go", rev: "r"}))
	m = m.pushOverlay(newBookmarkPopup(twoBookmarkItems()))
	ref := model.FileRef{Source: model.SourceCommit, Locator: "x", Path: "a.go"}
	m.bookmarkSwitcher().compareRef = &ref
	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.stackTop() != nil {
		t.Error("the history surface must be cleared so the diff owns the screen")
	}
	if !strings.Contains(mm.render(), "↔") {
		t.Fatal("compare diff must be visible over a history/blame surface")
	}
}

func TestBookmarksLoadErrorClearsPendingCompare(t *testing.T) {
	m := footerModel()
	m.pendingCompare = &pendingCompare{ref: model.FileRef{Path: "a.go"}, label: "x"}
	u, _ := m.Update(bookmarksLoadedMsg{err: errors.New("boom")})
	if u.(Model).pendingCompare != nil {
		t.Error("a failed bookmark load must clear pendingCompare")
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

func TestCompareAgainstWorkingDirRowOpensDiff(t *testing.T) {
	m := footerModel()
	// A focused commit file: the diff view with a rev makes focusedBookmark
	// yield a committed ref (Path = the diff title).
	m.diffView = &diffView{title: "a.go", rev: "abc1234"}
	r, ok := m.compareAgainstWorkingDirRow()
	if !ok {
		t.Fatal("row should be present for a focused commit file")
	}
	if r.label != "Compare against working dir" {
		t.Fatalf("label = %q", r.label)
	}
	u, cmd := r.run(m)
	mm := u.(Model)
	if mm.diffView == nil || mm.diffView.title != "a.go ↔ working" {
		t.Fatalf("diff view = %+v", mm.diffView)
	}
	if mm.diffTag != "cmpwd:a.go" {
		t.Fatalf("diffTag = %q, want cmpwd:a.go", mm.diffTag)
	}
	if cmd == nil {
		t.Fatal("expected a load command")
	}
}

func TestCompareAgainstWorkingDirRowAbsentForWorkingFile(t *testing.T) {
	m := footerModel()
	// A working-tree file is focused (diff view with no rev) → comparing it
	// against the working tree is itself-vs-itself, so the row is gated off.
	m.diffView = &diffView{title: "a.go"} // rev "" → unstaged source
	if _, ok := m.compareAgainstWorkingDirRow(); ok {
		t.Fatal("row should be absent for a working-tree file")
	}
}
