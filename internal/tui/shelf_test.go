package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

func TestShelfRowsContent(t *testing.T) {
	m := New(nil)
	m.shelfEntries = []model.ShelfEntry{
		{ID: "unstaged-a-b-aabbccdd", Origin: model.FileAddress{State: model.StateUnstaged, Worktree: "/repo", Path: "a/b.go"}, SHA: "aabbccddeeff"},
		{ID: "staged-readme-11223344", Origin: model.FileAddress{State: model.StateStaged, Worktree: "/repo", Path: "README.md"}, SHA: "1122334455"},
	}
	rows := m.shelfRows()
	if len(rows) != 2 {
		t.Fatalf("shelfRows len = %d", len(rows))
	}
	if !strings.Contains(rows[0], "a/b.go") || !strings.Contains(rows[0], "unstaged") {
		t.Fatalf("row0 missing fields: %q", rows[0])
	}
	if !strings.Contains(rows[1], "README.md") || !strings.Contains(rows[1], "staged") {
		t.Fatalf("row1 missing fields: %q", rows[1])
	}
}

func TestTabBarLabelIncludesShelf(t *testing.T) {
	if got := tabBarLabel(panelShelf); !strings.Contains(got, "[Shelf]") {
		t.Fatalf("active Shelf: %q", got)
	}
	if got := tabBarLabel(panelBranches); !strings.HasSuffix(got, " S") {
		t.Fatalf("Shelf short label missing when inactive: %q", got)
	}
}

func TestAddToShelfRowOnFilesPanel(t *testing.T) {
	m := filesMenuModel() // panelFiles focused with one tracked file
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "shelf-add"); !ok {
		t.Fatalf("Add to shelf missing from menu on Files panel")
	}
	a, ok := m.focusedShelfAddress()
	if !ok || a.State != model.StateUnstaged || a.Path != "dir/f.txt" || a.Worktree != "/wt" {
		t.Fatalf("focusedShelfAddress = %+v ok=%v, want unstaged dir/f.txt @ /wt", a, ok)
	}
}

func TestShelfAddCaptureFromBlame(t *testing.T) {
	// Working-tree blame (ctx.rev == "") is not a shelf-capture surface — this
	// mirrors bookmark capture (focusedBookmark guards rev==""); shelf the
	// working file from the Files panel instead. Guards against reintroducing a
	// StateCommitted{Commit:""} address that would resolve to the index blob.
	m := footerModel().pushSurface(blameFixture()) // ctx.rev == ""
	if _, ok := m.focusedShelfAddress(); ok {
		t.Fatalf("working-tree blame should not offer shelf-add")
	}
	// A committed blame captures the commit (same bytes the old code shelved).
	m2 := footerModel().pushSurface(&blameView{ctx: navContext{path: "a.go", rev: "abc1234def"}})
	a, ok := m2.focusedShelfAddress()
	if !ok || a.State != model.StateCommitted || a.Commit != "abc1234def" || a.Path != "a.go" {
		t.Fatalf("committed blame capture = %+v ok=%v", a, ok)
	}
}

func TestAddToShelfRowAbsentWhenNoFileFocused(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.focusedShelfAddress(); ok {
		t.Fatalf("no file focused on Branches panel; focusedShelfAddress should be false")
	}
	if _, ok := findRow(availableActions(m), "shelf-add"); ok {
		t.Fatalf("Add to shelf should not appear with no file focused")
	}
}

func shelfTabModel() Model {
	m := footerModel()
	m.focus = panelShelf
	m.activeLeftTab = panelShelf
	m.shelfEntries = []model.ShelfEntry{{ID: "unstaged-a-go-deadbeef", Origin: model.FileAddress{State: model.StateUnstaged, Path: "a.go"}, SHA: "deadbeefcafe0000"}}
	m.sel[panelShelf] = 0
	return m
}

func TestShelfTabMenuRows(t *testing.T) {
	m := shelfTabModel()
	rows := availableActions(m)
	if _, ok := findRow(rows, "shelf-restore"); !ok {
		t.Fatalf("Restore to… missing from Shelf-tab menu")
	}
	if _, ok := findRow(rows, "shelf-remove"); !ok {
		t.Fatalf("Remove from shelf missing from Shelf-tab menu")
	}
}

func TestShelfRestorePopupRequiresDest(t *testing.T) {
	m := shelfTabModel()
	m.shelfRestorePopup = &shelfRestorePopup{entryID: "unstaged-a-go-deadbeef", origin: "a.go"}
	// Enter with an empty dest is a no-op (popup stays open).
	u, _ := m.updateShelfRestoreKey(keyMsg("enter"))
	m = u.(Model)
	if m.shelfRestorePopup == nil {
		t.Fatalf("empty dest should keep the popup open")
	}
	// Typing builds the destination.
	for _, r := range "out.txt" {
		u, _ = m.updateShelfRestoreKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	if m.shelfRestorePopup.dest != "out.txt" {
		t.Fatalf("dest = %q, want out.txt", m.shelfRestorePopup.dest)
	}
}

func TestShelfCompareOpensDiff(t *testing.T) {
	m := shelfTabModel()
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.diffView == nil {
		t.Fatalf("enter on a shelf entry should open a diff view")
	}
	if m.diffTag != "shelf:unstaged-a-go-deadbeef" {
		t.Fatalf("diffTag = %q, want shelf:<id>", m.diffTag)
	}
}

func TestShelfPairOpCompare(t *testing.T) {
	if ops := pairOpsFor(panelShelf); len(ops) != 1 || !ops[0].enabled || ops[0].open == nil {
		t.Fatalf("panelShelf should offer one enabled open-based Compare pair-op, got %+v", ops)
	}
}

func TestShelfCompareTwoOpensDiff(t *testing.T) {
	m := footerModel()
	m.focus = panelShelf
	m.shelfEntries = []model.ShelfEntry{
		{ID: "a", Origin: model.FileAddress{State: model.StateUnstaged, Path: "a.go"}, SHA: "aaaa1111bbbb"},
		{ID: "b", Origin: model.FileAddress{State: model.StateUnstaged, Path: "a.go"}, SHA: "cccc2222dddd"},
	}
	m, _ = m.openShelfCompareTwo("a", "b")
	if m.diffView == nil || m.diffTag != "shelf2:a:b" {
		t.Fatalf("two-entry compare should open a diff with tag shelf2:a:b, got tag=%q view=%v", m.diffTag, m.diffView)
	}
	if m.mark != nil {
		t.Fatalf("the mark should be consumed after compare")
	}
}

func TestShelfMarkThenCompareOpensPicker(t *testing.T) {
	m := shelfTabModel()
	m.shelfEntries = append(m.shelfEntries, model.ShelfEntry{ID: "b", Origin: model.FileAddress{State: model.StateUnstaged, Path: "b.go"}, SHA: "ffff"})
	// Mark entry 0, move to entry 1, press m again → pair-op picker.
	u, _ := m.Update(keyMsg("m"))
	m = u.(Model)
	if m.mark == nil {
		t.Fatalf("first m should set a mark")
	}
	m.sel[panelShelf] = 1
	u, _ = m.Update(keyMsg("m"))
	m = u.(Model)
	if m.pairPopup == nil {
		t.Fatalf("second m on another entry should open the pair-op picker")
	}
}

func TestShelfTabInCycle(t *testing.T) {
	m := New(nil)
	m.width, m.height = 80, 24
	m.focus = panelBranches
	send := func(k string) {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	// Branches -> Remotes -> Worktrees -> Shelf -> Branches (wrap).
	send("ctrl+right")
	send("ctrl+right")
	send("ctrl+right")
	if m.activeLeftTab != panelShelf || m.focus != panelShelf {
		t.Fatalf("3x ctrl+right: tab=%v focus=%v, want Shelf", m.activeLeftTab, m.focus)
	}
	send("ctrl+right")
	if m.activeLeftTab != panelBranches {
		t.Fatalf("4x ctrl+right: tab=%v, want Branches (wrap)", m.activeLeftTab)
	}
}
