package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Two commit bookmarks marked with m must dispatch an entry-compare resolve
// (a tea.Cmd), not a notice.
func TestBookmarkMarkTwoCommitEntries(t *testing.T) {
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
