package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

func shelfPopModel(entries ...model.ShelfEntry) Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m.shelfEntries = entries
	m = m.pushLayer(newShelfPopup(entries))
	return m
}

func shEntry(id, path string) model.ShelfEntry {
	return model.ShelfEntry{ID: id, Origin: model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: path}, SHA: id + "0000"}
}

func TestShelfPopupRendersOrigin(t *testing.T) {
	m := shelfPopModel(shEntry("a", "dir/x.go"))
	out := m.renderShelfPopupBox(m.shelfSwitcher())
	if !strings.Contains(out, "dir/x.go") || !strings.Contains(out, "Shelf") {
		t.Fatalf("popup missing content:\n%s", out)
	}
}

func TestShelfPopupZCyclesMode(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.Update(keyMsg("z"))
	m = mm.(Model)
	if m.shelfSwitcher().mode != modeWrap {
		t.Fatalf("z should cycle to wrap, got %v", m.shelfSwitcher().mode)
	}
}

func TestShelfPopupEnterJumps(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.diffLayer() == nil || m.diffTag != "shelf:a" {
		t.Fatalf("enter should open the shelf-vs-worktree diff, tag=%q", m.diffTag)
	}
	// The diff is pushed over the switcher; esc from the diff returns to it.
	if m.shelfSwitcher() == nil {
		t.Fatalf("the shelf switcher must remain on the stack beneath the diff")
	}
}

func TestShelfPopupRemoveConfirms(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	if m.modal == nil {
		t.Fatalf("x should open a remove-confirm modal")
	}
}

func TestShelfPopupRestoreOpensDest(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, _ := m.Update(keyMsg("p"))
	m = mm.(Model)
	if shelfRestoreOf(m) == nil || shelfRestoreOf(m).entryID != "a" {
		t.Fatalf("p should open the restore-destination popup")
	}
}

func TestCompareAgainstShelfMenuRow(t *testing.T) {
	m := filesMenuModel()
	m.currentWorktree = "/wt"
	r, ok := findRow(availableActions(m), "shelf-compare-against")
	if !ok {
		t.Fatalf("Compare against shelf missing on Files panel")
	}
	mm, cmd := r.run(m)
	m = mm.(Model)
	if m.pendingCompare == nil || m.pendingCompare.target != compareShelf {
		t.Fatalf("running it should set a shelf-targeted pendingCompare, got %+v", m.pendingCompare)
	}
	if cmd == nil {
		t.Fatalf("it should load the shelf")
	}
}

func TestShelfPopupCAgainstBookmark(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	mm, cmd := m.Update(keyMsg("c"))
	m = mm.(Model)
	if m.shelfSwitcher() == nil {
		t.Fatalf("c should keep the shelf switcher on the stack (the bookmark picker stacks on top)")
	}
	if m.pendingCompare == nil || m.pendingCompare.target != compareBookmark {
		t.Fatalf("c should set a bookmark-targeted pendingCompare, got %+v", m.pendingCompare)
	}
	if cmd == nil {
		t.Fatalf("c should load bookmarks")
	}
}

func TestShelfCompareModeEnterDiffs(t *testing.T) {
	m := shelfPopModel(shEntry("a", "x.go"))
	m.shelfSwitcher().compareRef = &model.FileRef{Source: model.SourceUnstaged, Path: "focused.go"}
	m.shelfSwitcher().compareLabel = "wt:wt / unstaged / focused.go"
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.diffLayer() == nil || !strings.HasPrefix(m.diffTag, "cmpsh:") {
		t.Fatalf("enter in compare mode should diff focused vs shelf, tag=%q", m.diffTag)
	}
}

func TestPendingCompareStampsShelfPopup(t *testing.T) {
	m := footerModel()
	m.width, m.height = 100, 30
	m.pendingCompare = &pendingCompare{ref: model.FileRef{Source: model.SourceUnstaged, Path: "f.go"}, label: "wt / unstaged / f.go", target: compareShelf}
	u, _ := m.Update(shelfLoadedMsg{entries: []model.ShelfEntry{shEntry("a", "x.go")}, open: true})
	m = u.(Model)
	if m.shelfSwitcher() == nil || m.shelfSwitcher().compareRef == nil {
		t.Fatalf("a shelf-targeted pendingCompare should stamp compareRef onto the new popup")
	}
	if m.pendingCompare != nil {
		t.Fatalf("pendingCompare should be consumed")
	}
}

func TestPendingCompareStampsBookmarkPopup(t *testing.T) {
	m := footerModel()
	m.width, m.height = 100, 30
	m.pendingCompare = &pendingCompare{ref: model.FileRef{Source: model.SourceShelf, Locator: "a", Path: "x.go"}, label: "shelf #a", target: compareBookmark}
	u, _ := m.Update(bookmarksLoadedMsg{items: []model.Bookmark{{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "y.go"}}})
	m = u.(Model)
	if m.bookmarkSwitcher() == nil || m.bookmarkSwitcher().compareRef == nil {
		t.Fatalf("a bookmark-targeted pendingCompare should stamp compareRef onto the new popup")
	}
	if m.pendingCompare != nil {
		t.Fatalf("pendingCompare should be consumed")
	}
}

func TestShelfPopupMarkThenCompare(t *testing.T) {
	m := shelfPopModel(shEntry("a", "a.go"), shEntry("b", "b.go"))
	mm, _ := m.Update(keyMsg("m"))
	m = mm.(Model)
	if m.shelfSwitcher() == nil || m.shelfSwitcher().markID != "a" {
		t.Fatalf("first m should mark entry a")
	}
	m.shelfSwitcher().sel = 1
	mm, _ = m.Update(keyMsg("m"))
	m = mm.(Model)
	if m.diffLayer() == nil || m.diffTag != "shelf2:a:b" {
		t.Fatalf("second m should open the two-entry diff, tag=%q", m.diffTag)
	}
}

func TestShelfEntryDisplayLabel(t *testing.T) {
	labeled := model.ShelfEntry{
		Kind:   model.ShelfKindCommit,
		Origin: model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5"},
		Label:  "my fix",
	}
	got := shelfEntryDisplay(labeled)
	if !strings.Contains(got, "my fix") || !strings.HasSuffix(got, "— my fix") {
		t.Fatalf("labeled display = %q, want it to end with '— my fix'", got)
	}
	plain := model.ShelfEntry{
		Kind:   model.ShelfKindCommit,
		Origin: model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5"},
	}
	if shelfEntryDisplay(plain) != plain.Origin.Display() {
		t.Fatalf("unlabeled display should equal Origin.Display()")
	}
}

func TestShelfPopupMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &shelfPopup{}
	for i := 0; i < 30; i++ { // more than the fixed cap of 12
		p.items = append(p.items, model.ShelfEntry{})
		p.rows = append(p.rows, fmt.Sprintf("some/long/origin/path/file%d", i))
	}

	normal := m.renderShelfPopupBox(p)
	p.maximized = true
	maxed := m.renderShelfPopupBox(p)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

func TestShelfPopupTKeyDoesNotMaximizeWhileFiltering(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	p := &shelfPopup{filtering: true}
	p.update(m, runeKey("T"))
	if p.maximized {
		t.Fatal(`"T" while filtering must not maximize`)
	}
	if p.filter != "T" {
		t.Fatalf(`"T" while filtering must be a literal char; filter=%q`, p.filter)
	}
}
