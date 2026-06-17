package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/hunkpick"
)

// pressDot opens the menu via the real Update dispatch from a model already set
// up with some window/surface active.
func pressDot(m Model) Model {
	u, _ := m.Update(keyMsg("."))
	return u.(Model)
}

func TestDotOpensMenuFromFilesView(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.filesView = &contentPopup{lines: []contentLine{{text: "a.go", path: "a.go"}}}
	m.filesTreeFocused = true
	if pressDot(m).actionMenu == nil {
		t.Fatal(". must open the action menu from the file tree")
	}
}

func TestDotOpensMenuFromDiffView(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.diffView = &diffView{title: "a.go", rev: "abc123"}
	if pressDot(m).actionMenu == nil {
		t.Fatal(". must open the action menu from the diff view")
	}
}

func TestDotOpensMenuFromStashView(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.stashView = &stashView{}
	m.focus = panelCommits
	if pressDot(m).actionMenu == nil {
		t.Fatal(". must open the action menu from the stash list")
	}
}

func TestDotOpensMenuFromHistory(t *testing.T) {
	m := footerModel()
	m.loading = false
	m = m.pushSurface(newHistoryView(navContext{path: "a.go", rev: "abc"}))
	if pressDot(m).actionMenu == nil {
		t.Fatal(". must open the action menu from the history view")
	}
}

func TestDotOpensMenuFromBlame(t *testing.T) {
	m := footerModel()
	m.loading = false
	m = m.pushSurface(newBlameView(navContext{path: "a.go", rev: "abc"}))
	if pressDot(m).actionMenu == nil {
		t.Fatal(". must open the action menu from the blame view")
	}
}

// The dispatch move: when the menu is open over a window, its keys reach the
// menu (esc closes it) and the underlying window survives.
func TestMenuOwnsKeysOverDiffView(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.diffView = &diffView{title: "a.go", rev: "abc"}
	m = m.openActionMenu()
	u, _ := m.Update(keyMsg("esc"))
	mm := u.(Model)
	if mm.actionMenu != nil {
		t.Error("esc should close the menu, not be eaten by the diff view")
	}
	if mm.diffView == nil {
		t.Error("the diff view must survive closing the menu")
	}
}

func TestMenuOwnsKeysOverHistory(t *testing.T) {
	m := footerModel()
	m.loading = false
	m = m.pushSurface(newHistoryView(navContext{path: "a.go", rev: "abc"}))
	m = m.openActionMenu()
	u, _ := m.Update(keyMsg("esc"))
	mm := u.(Model)
	if mm.actionMenu != nil {
		t.Error("esc should close the menu, not be eaten by the history view")
	}
	if _, ok := mm.stackTop().(*historyView); !ok {
		t.Error("the history view must survive closing the menu")
	}
}

func TestDotInertOverModal(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.modal = &decisionState{}
	if pressDot(m).actionMenu != nil {
		t.Error(". must not open the menu over a decision modal")
	}
}

func TestDotInertOverRepoPopup(t *testing.T) {
	m := footerModel()
	m.loading = false
	m.repoPopup = &repoPopup{}
	if pressDot(m).actionMenu != nil {
		t.Error(". must not open the menu over a popup")
	}
}

func TestDotInertOverIrebaseEditor(t *testing.T) {
	m := footerModel()
	m.loading = false
	m = m.pushSurface(newIrebaseEditor("feat", "main", nil, "gg"))
	if pressDot(m).actionMenu != nil {
		t.Error(". must not open the menu over the interactive-rebase editor")
	}
}

func TestDotInertOverHunkPicker(t *testing.T) {
	m := footerModel()
	m.loading = false
	m = m.pushSurface(newStagePicker("f.txt", &hunkpick.Doc{}))
	if pressDot(m).actionMenu != nil {
		t.Error(". must not open the menu over the hunk picker")
	}
}
