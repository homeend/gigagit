package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/model"
)

// Right-click = "select the row under the cursor, then open the . menu".

func TestRightClickPanelSelectsAndOpensActionMenu(t *testing.T) {
	m := mouseModel() // commits panel x=26.., rows from y=3
	u, _ := m.Update(mouseMsg(30, 4, tea.MouseButtonRight))
	m2 := u.(Model)
	if m2.focus != panelCommits {
		t.Fatalf("focus = %v, want commits", m2.focus)
	}
	if m2.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want the clicked row 1", m2.sel[panelCommits])
	}
	if m2.actionMenu == nil {
		t.Fatal("right-click should open the action menu")
	}
}

func TestRightClickOffPanelsOpensNothing(t *testing.T) {
	m := mouseModel()
	u, _ := m.Update(mouseMsg(5, 0, tea.MouseButtonRight)) // header row
	if u.(Model).actionMenu != nil {
		t.Fatal("right-click off the panels must not open the menu")
	}
}

func TestRightClickTextPopupInert(t *testing.T) {
	p := &branchPopup{}
	m := mouseModel().pushLayer(p)
	u, _ := m.Update(mouseMsg(40, 12, tea.MouseButtonRight))
	m2 := u.(Model)
	if m2.actionMenu != nil {
		t.Fatal("right-click over a text popup must not open the menu")
	}
	if m2.topLayer() != layer(p) {
		t.Fatal("the popup must stay on top")
	}
}

// Double-click = "select, then enter" on the same control.

func TestDoubleClickCommitDrillsIn(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.width, m.height = 80, 24
	u, _ := m.Update(mouseMsg(30, 3, tea.MouseButtonLeft))
	u2, _ := u.(Model).Update(mouseMsg(30, 3, tea.MouseButtonLeft))
	m2 := u2.(Model)
	if m2.filesView == nil {
		t.Fatal("double-click on a commit should drill in like enter")
	}
	if !m2.filesTreeFocused {
		t.Fatal("the drill-in should land focus on the tree, like enter")
	}
}

func TestDoubleClickDifferentRowsDoesNotEnter(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.width, m.height = 80, 24
	u, _ := m.Update(mouseMsg(30, 3, tea.MouseButtonLeft))
	u2, _ := u.(Model).Update(mouseMsg(30, 4, tea.MouseButtonLeft))
	m2 := u2.(Model)
	if m2.filesView != nil {
		t.Fatal("two quick clicks on different rows are not a double-click")
	}
	if m2.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want the second clicked row 1", m2.sel[panelCommits])
	}
}

func TestDoubleClickStaleTimerDoesNotEnter(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.width, m.height = 80, 24
	u, _ := m.Update(mouseMsg(30, 3, tea.MouseButtonLeft))
	m1 := u.(Model)
	m1.lastClickAt = time.Now().Add(-time.Second)
	u2, _ := m1.Update(mouseMsg(30, 3, tea.MouseButtonLeft))
	if u2.(Model).filesView != nil {
		t.Fatal("clicks slower than the double-click window must not enter")
	}
}

func TestTripleClickFiresEnterOnce(t *testing.T) {
	m := mouseModel()
	// A completing double-click clears the record: the third click starts a
	// fresh cycle instead of firing enter again.
	m, double := m.registerClick(clickTarget{zone: zonePanel, panel: panelCommits, row: 0})
	if double {
		t.Fatal("first click is never a double")
	}
	m, double = m.registerClick(clickTarget{zone: zonePanel, panel: panelCommits, row: 0})
	if !double {
		t.Fatal("second click on the same target is a double")
	}
	_, double = m.registerClick(clickTarget{zone: zonePanel, panel: panelCommits, row: 0})
	if double {
		t.Fatal("third click must start a fresh cycle, not fire again")
	}
}

func TestDoubleClickActionMenuRunsSelectedRow(t *testing.T) {
	m := mouseModel()
	ran := false
	m.actionMenu = &actionMenu{rows: []actionRow{{id: "spy", label: "spy",
		run: func(mm Model) (tea.Model, tea.Cmd) { ran = true; return mm, nil }}}}
	u, _ := m.Update(mouseMsg(40, 12, tea.MouseButtonLeft))
	if u.(Model).actionMenu == nil || ran {
		t.Fatal("a single click must not run a menu row")
	}
	u2, _ := u.(Model).Update(mouseMsg(40, 12, tea.MouseButtonLeft))
	if !ran {
		t.Fatal("double-click should run the selected menu row like enter")
	}
	if u2.(Model).actionMenu != nil {
		t.Fatal("running the row closes the menu")
	}
}

func TestDoubleClickContentPopupClosesLikeEnter(t *testing.T) {
	m := mouseModel().pushLayer(newContentPopup("t", []contentLine{{text: "x"}}))
	u, _ := m.Update(mouseMsg(40, 12, tea.MouseButtonLeft))
	if u.(Model).topLayer() == nil {
		t.Fatal("a single click must not close the popup")
	}
	u2, _ := u.(Model).Update(mouseMsg(40, 12, tea.MouseButtonLeft))
	if u2.(Model).topLayer() != nil {
		t.Fatal("double-click acts as enter: the content popup closes")
	}
}

func TestDoubleClickTextPopupInert(t *testing.T) {
	p := &branchPopup{}
	m := mouseModel().pushLayer(p)
	u, _ := m.Update(mouseMsg(40, 12, tea.MouseButtonLeft))
	u2, _ := u.(Model).Update(mouseMsg(40, 12, tea.MouseButtonLeft))
	if u2.(Model).topLayer() != layer(p) {
		t.Fatal("double-click over a text popup must not act on it")
	}
}

// The allowlists are the safety boundary: list-style controls accept a
// double-click as enter, the full-screen readers accept right-click as ".",
// and everything else — text-entry popups above all — stays mouse-inert.
func TestClickLayerAllowlists(t *testing.T) {
	for _, c := range []struct {
		l           layer
		enter, menu bool
	}{
		{&repoPopup{}, true, false},
		{&bookmarkPopup{}, true, false},
		{&shelfPopup{}, true, false},
		{&commandPalette{}, true, false},
		{&pairOpPopup{}, true, false},
		{&contentPopup{}, true, false},
		{&historyView{}, false, true},
		{&blameView{}, false, true},
		{&diffView{}, false, true},
		{&branchPopup{}, false, false},   // text entry: enter submits
		{&commitPopup{}, false, false},   // text entry: enter submits
		{&worktreePopup{}, false, false}, // text entry: enter submits
	} {
		if got := clickEnterLayer(c.l); got != c.enter {
			t.Errorf("clickEnterLayer(%T) = %v, want %v", c.l, got, c.enter)
		}
		if got := rightClickMenuLayer(c.l); got != c.menu {
			t.Errorf("rightClickMenuLayer(%T) = %v, want %v", c.l, got, c.menu)
		}
	}
}

func TestRightClickHistoryViewOpensMenu(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.width, m.height = 80, 24
	m = m.pushLayer(&historyView{ctx: navContext{path: "a.txt", rev: "HEAD"},
		commits: []model.FileCommit{{Commit: model.Commit{Hash: "1111111"}, Path: "a.txt"}}})
	u, _ := m.Update(mouseMsg(40, 12, tea.MouseButtonRight))
	if u.(Model).actionMenu == nil {
		t.Fatal("right-click on the history view should open its action menu, like .")
	}
}

func TestRightClickFilesViewTreeOpensMenu(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.width, m.height = 80, 24
	m.focus = panelCommits
	m, _ = pressEnter(m) // open the files view
	if m.filesView == nil {
		t.Fatal("setup: enter should open the files view")
	}
	u, _ := m.Update(mouseMsg(5, 3, tea.MouseButtonRight))
	if u.(Model).actionMenu == nil {
		t.Fatal("right-click on the files tree should open the action menu")
	}
}
