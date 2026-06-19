package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestBookmarkDisplayString(t *testing.T) {
	got := bookmarkDisplay(model.Bookmark{State: model.StateCommitted, Commit: "a1b2c3d4e5", Path: "src/x.go", Branch: "feat"})
	if !strings.Contains(got, "src/x.go") || !strings.Contains(got, "a1b2c3d") || !strings.Contains(got, "feat") {
		t.Fatalf("display = %q", got)
	}
}

// Full path: g opens (async load), the loaded msg builds the popup, View renders it.
func TestBookmarkPopupOpenAndRenderFullPath(t *testing.T) {
	m := footerModel()
	m.width, m.height = 100, 30
	u, cmd := m.Update(keyMsg("g"))
	m = u.(Model)
	if cmd == nil {
		t.Fatal("g should fire a bookmark-load command")
	}
	u, _ = m.Update(bookmarksLoadedMsg{items: []model.Bookmark{
		{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a/b.go"},
	}})
	m = u.(Model)
	if m.bookmarkPopup == nil {
		t.Fatal("loaded msg should open the popup")
	}
	out := m.View()
	if !strings.Contains(out, "a/b.go") {
		t.Fatalf("popup not rendered:\n%s", out)
	}
}

func TestBookmarkRowOnFilesPanel(t *testing.T) {
	m := filesMenuModel() // panelFiles focused with one tracked file "dir/f.txt"
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "bookmark-add"); !ok {
		t.Fatalf("Bookmark this file missing on Files panel")
	}
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateUnstaged || b.Worktree != "/wt" || b.Path != "dir/f.txt" {
		t.Fatalf("focusedBookmark = %+v ok=%v", b, ok)
	}
}

func TestBookmarkRowAbsentWhenNoFile(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.focusedBookmark(); ok {
		t.Fatalf("no file focused → focusedBookmark should be false")
	}
}
