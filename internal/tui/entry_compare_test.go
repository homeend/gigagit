package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// Two commit bookmarks marked with m must dispatch an entry-compare resolve
// (a tea.Cmd), not a notice.
func TestBookmarkMarkTwoCommitEntries(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	a := model.Bookmark{ID: "b1", Commit: "1111111111111111111111111111111111111111", State: model.StateCommitted}
	b := model.Bookmark{ID: "b2", Commit: "2222222222222222222222222222222222222222", State: model.StateCommitted}
	p := newBookmarkPopup([]model.Bookmark{a, b})
	p.markID = "b1"
	p.sel = 1
	m = m.pushLayer(p)

	mm, cmd := m.bookmarkMark()
	if cmd == nil {
		t.Fatal("marking a second commit bookmark must dispatch the resolve cmd")
	}
	if mm.statusMsg != "" {
		t.Fatalf("unexpected notice: %q", mm.statusMsg)
	}
}

// A mixed pair (file mark + commit second) is a notice, not a compare.
func TestBookmarkMarkMixedKindsRefused(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	a := model.Bookmark{ID: "b1", Path: "f.go", State: model.StateUnstaged}
	b := model.Bookmark{ID: "b2", Commit: "2222222222222222222222222222222222222222", State: model.StateCommitted}
	p := newBookmarkPopup([]model.Bookmark{a, b})
	p.markID = "b1"
	p.sel = 1
	m = m.pushLayer(p)

	mm, cmd := m.bookmarkMark()
	if cmd != nil {
		t.Fatal("a mixed pair must not dispatch a compare")
	}
	if mm.statusMsg == "" {
		t.Fatal("a mixed pair must set a notice")
	}
}

// A stale entryCompareMsg (gen mismatch) is dropped.
func TestEntryCompareGenGuard(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m.entryCompareGen = 5
	upd, _ := m.Update(entryCompareMsg{gen: 4, left: model.Endpoint{Kind: model.EndpointCommit, Hash: "a"}, right: model.Endpoint{Kind: model.EndpointCommit, Hash: "b"}})
	mm := upd.(Model)
	if mm.filesView != nil {
		t.Fatal("a stale resolve must not open the compare view")
	}
}

// m on a lone shelved commit entry (nothing marked yet) is now a plain
// toggle-mark like any other entry kind — the guard only ever blocked "m"
// when there was no second entry to compare against.
func TestShelfPopupMarkTogglesOnLoneCommitEntry(t *testing.T) {
	t.Parallel()
	m := shelfPopModel(shCommitEntry("ce"))
	mm, cmd := m.Update(keyMsg("m"))
	m = mm.(Model)
	if cmd != nil {
		t.Fatal("marking the only entry must not dispatch a command")
	}
	if m.statusMsg != "" {
		t.Fatalf("unexpected notice: %q", m.statusMsg)
	}
	if m.shelfSwitcher().markID != "ce" {
		t.Fatalf("m must mark the lone commit entry, markID=%q", m.shelfSwitcher().markID)
	}
}

// Same commit on both sides (and not two distinct shelf entries) is a notice.
func TestEntryCompareSelfCompareNotice(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	side := entrySide{sha: "3333333333333333333333333333333333333333", label: "x"}
	mm, cmd := m.startEntryCompare(side, side)
	if cmd != nil {
		t.Fatal("self-compare must not dispatch")
	}
	if mm.statusMsg == "" {
		t.Fatal("self-compare must set a notice")
	}
}

// Two DIFFERENT shelf entries of the SAME commit sha must dispatch a compare
// (the distinctShelves exception) rather than the self-compare notice.
func TestEntryCompareDistinctShelvesOfSameCommit(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	sha := "4444444444444444444444444444444444444444"
	left := entrySide{sha: sha, shelfID: "s1", label: "shelf #s1"}
	right := entrySide{sha: sha, shelfID: "s2", label: "shelf #s2"}
	mm, cmd := m.startEntryCompare(left, right)
	if cmd == nil {
		t.Fatal("two distinct shelf entries of the same commit must dispatch a compare")
	}
	if mm.statusMsg != "" {
		t.Fatalf("unexpected notice: %q", mm.statusMsg)
	}
}

// c on a commit bookmark must arm a commit-flavored pendingCompare targeting
// the shelf picker.
func TestBookmarkCommitCrossCompareArm(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	b := model.Bookmark{ID: "b1", Commit: "1111111111111111111111111111111111111111", State: model.StateCommitted}
	p := newBookmarkPopup([]model.Bookmark{b})
	m = m.pushLayer(p)

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}) // keys route to the top layer through Model.Update
	got := mm.(Model)
	if got.pendingCompare == nil || got.pendingCompare.entry == nil {
		t.Fatalf("pendingCompare = %+v, want a commit-flavored arm", got.pendingCompare)
	}
	if got.pendingCompare.target != compareShelf {
		t.Errorf("target = %v, want compareShelf", got.pendingCompare.target)
	}
	if cmd == nil {
		t.Error("c must dispatch the shelf load")
	}
}

// In commit-flavored compare mode, enter on a FILE entry is a notice.
func TestShelfCompareModeCommitVsFileRefused(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	fileEntry := model.ShelfEntry{ID: "s1", Origin: model.FileAddress{State: model.StateUnstaged, Path: "f.go"}}
	p := newShelfPopup([]model.ShelfEntry{fileEntry})
	side := entrySide{sha: "1111111111111111111111111111111111111111", label: "bm"}
	p.compareEntry = &side
	m = m.pushLayer(p)

	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := mm.(Model)
	if cmd != nil {
		t.Fatal("commit-vs-file must not dispatch a compare")
	}
	if got.statusMsg == "" {
		t.Fatal("commit-vs-file must set a notice")
	}
}
