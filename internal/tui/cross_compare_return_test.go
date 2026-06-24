package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// The cross-store compare opens the *other* switcher to pick the right-hand
// side. That child must STACK on top of the originating switcher, so esc in the
// child returns to the parent — the whole point of the overlay stack. These
// guard the regression where the `c` handler popped the parent before loading
// the child, dropping the user to nothing on esc.

func TestBookmarkCompareShelfEscReturnsToBookmark(t *testing.T) {
	m := switcherModel(t)

	// c: compare the highlighted bookmark against a shelf entry.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = u.(Model)
	if m.bookmarkSwitcher() == nil {
		t.Fatal("c must keep the bookmark switcher on the stack (not pop it)")
	}

	// The shelf load completes → the shelf popup is pushed in compare mode.
	u, _ = m.Update(shelfLoadedMsg{entries: []model.ShelfEntry{{ID: "s1"}}, open: true})
	m = u.(Model)
	sp, ok := m.topLayer().(*shelfPopup)
	if !ok {
		t.Fatalf("shelf load must push the shelf popup on top, got %T", m.topLayer())
	}
	if sp.compareRef == nil {
		t.Fatal("the shelf popup must be in compare mode")
	}

	// esc the shelf popup → back to the bookmark switcher beneath.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if _, ok := m.topLayer().(*bookmarkPopup); !ok {
		t.Fatalf("esc in the compare-shelf popup must return to the bookmark switcher, got %T", m.topLayer())
	}
}

func TestShelfCompareBookmarkEscReturnsToShelf(t *testing.T) {
	m := shelfSwitcherModel()

	// c: compare the highlighted shelf entry against a bookmark.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = u.(Model)
	if m.shelfSwitcher() == nil {
		t.Fatal("c must keep the shelf switcher on the stack (not pop it)")
	}

	// The bookmark load completes → the bookmark popup is pushed in compare mode.
	u, _ = m.Update(bookmarksLoadedMsg{items: []model.Bookmark{{ID: "b1", State: model.StateUnstaged, Path: "x.go"}}})
	m = u.(Model)
	bp, ok := m.topLayer().(*bookmarkPopup)
	if !ok {
		t.Fatalf("bookmark load must push the bookmark popup on top, got %T", m.topLayer())
	}
	if bp.compareRef == nil {
		t.Fatal("the bookmark popup must be in compare mode")
	}

	// esc the bookmark popup → back to the shelf switcher beneath.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if _, ok := m.topLayer().(*shelfPopup); !ok {
		t.Fatalf("esc in the compare-bookmark popup must return to the shelf switcher, got %T", m.topLayer())
	}
}
