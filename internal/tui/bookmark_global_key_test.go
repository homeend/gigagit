package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// The bookmark quick-switcher is a global action: `g` must open it from every
// navigable window, not just the base panels. The file tree was the reported
// gap — its key handler swallows every key before the base dispatch.

func twoBookmarks() *bookmarkPopup {
	return newBookmarkPopup([]model.Bookmark{
		{Path: "a.go"},
		{Path: "b.go"},
	})
}

func TestBookmarkKeyOpensFromFileTree(t *testing.T) {
	m := footerModel()
	m.filesView = &contentPopup{lines: []contentLine{{text: "f.go", path: "f.go"}}}
	m.filesTreeFocused = true
	_, cmd := m.Update(keyMsg("g"))
	if cmd == nil {
		t.Fatal("g in the file tree must open the bookmark switcher (load cmd), got nil")
	}
}

func TestBookmarkKeyOpensFromDiffView(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "a.go"}
	_, cmd := m.Update(keyMsg("g"))
	if cmd == nil {
		t.Fatal("g in the diff view must open the bookmark switcher, got nil")
	}
}

func TestBookmarkKeyOpensFromStash(t *testing.T) {
	m := footerModel()
	m.stashView = &stashView{}
	m.focus = panelCommits
	_, cmd := m.Update(keyMsg("g"))
	if cmd == nil {
		t.Fatal("g over the stash list must open the bookmark switcher, got nil")
	}
}

func TestBookmarkKeyOpensFromHistory(t *testing.T) {
	m := footerModel().pushSurface(newHistoryView(navContext{path: "a.go"}))
	_, cmd := m.Update(keyMsg("g"))
	if cmd == nil {
		t.Fatal("g in the history view must open the bookmark switcher, got nil")
	}
}

func TestBookmarkKeyOpensFromBlame(t *testing.T) {
	m := footerModel().pushSurface(newBlameView(navContext{path: "a.go"}))
	_, cmd := m.Update(keyMsg("g"))
	if cmd == nil {
		t.Fatal("g in the blame view must open the bookmark switcher, got nil")
	}
}

// Dispatch hoist: once open over a diff view (or stack surface), keys must
// reach the popup, not the window underneath. Without the hoist, esc would
// close the diff and leave the popup orphaned.
func TestBookmarkPopupReceivesKeysOverDiffView(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "a.go"}
	m = m.pushOverlay(twoBookmarks())
	u, _ := m.Update(keyMsg("esc"))
	mm := u.(Model)
	if mm.bookmarkSwitcher() != nil {
		t.Error("esc should close the bookmark popup over a diff view")
	}
	if mm.diffView == nil {
		t.Error("esc should reach the popup, not close the diff view underneath")
	}
}

// Render hoist: the popup must paint over the diff view's full-screen render.
func TestBookmarkPopupRendersOverDiffView(t *testing.T) {
	m := footerModel()
	m.diffView = &diffView{title: "a.go"}
	m = m.pushOverlay(twoBookmarks())
	if !strings.Contains(m.render(), "Bookmarks") {
		t.Error("bookmark popup must render over the diff view")
	}
}

func TestBookmarkPopupRendersOverHistory(t *testing.T) {
	m := footerModel().pushSurface(newHistoryView(navContext{path: "a.go"}))
	m = m.pushOverlay(twoBookmarks())
	if !strings.Contains(m.render(), "Bookmarks") {
		t.Error("bookmark popup must render over a stack surface")
	}
}
