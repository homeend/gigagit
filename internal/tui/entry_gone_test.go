package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A bookmark whose blob is gone: the loading diff opened over the switcher is
// closed again (the user lands back ON the switcher, not on a view reading
// "error: git cat-file … bad file") and the notice is sticky.
func TestGoneFileBookmarkReturnsToSwitcherWithStickyNotice(t *testing.T) {
	t.Parallel()
	b := model.Bookmark{ID: "b1", State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: "g.txt", SHA: "3e75"}
	m := bookmarkCopyModel(b)
	m, _ = m.openPickerDiff(&diffView{title: b.Path, loading: true}, "bookmark:b1", nil)

	gone := &domain.EntryGoneError{What: "g.txt @ a1b2c3d", Cause: errors.New("bad file")}
	mm, cmd := m.Update(entryGoneMsg{tag: "bookmark:b1", err: gone})
	m = mm.(Model)
	if m.diffLayer() != nil {
		t.Fatal("the loading diff must be closed — there is nothing to show in it")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("the switcher must be back on top")
	}
	if !strings.Contains(m.statusMsg, "g.txt @ a1b2c3d is no longer available") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
	if cmd == nil {
		t.Fatal("a sticky notice must arm its expiry tick")
	}

	// Sticky: the next keypress must NOT wipe it (an ordinary status message
	// dies on the first key, i.e. before anybody has read it).
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mm.(Model)
	if !strings.Contains(m.statusMsg, "no longer available") {
		t.Fatalf("the notice did not survive a keypress: %q", m.statusMsg)
	}
	// …and the expiry tick clears it.
	mm, _ = m.Update(stickyExpiredMsg{gen: m.stickyGen})
	m = mm.(Model)
	if m.statusMsg != "" {
		t.Fatalf("the notice outlived its tick: %q", m.statusMsg)
	}
}

// A stale result (the view was closed, another diff opened) changes nothing.
func TestGoneEntryMsgForAnotherViewIsDropped(t *testing.T) {
	t.Parallel()
	m := bookmarkCopyModel(model.Bookmark{ID: "b1", State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: "g.txt"})
	m, _ = m.openPickerDiff(&diffView{title: "other", loading: true}, "bookmark:other", nil)
	mm, _ := m.Update(entryGoneMsg{tag: "bookmark:b1", err: &domain.EntryGoneError{What: "x", Cause: errors.New("c")}})
	if mm.(Model).diffLayer() == nil {
		t.Fatal("a stale gone-result must not close somebody else's diff")
	}
}

// An old tick must not clear a NEWER sticky notice.
func TestStaleStickyTickKeepsTheNewerNotice(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m, _ = m.stickyNotice("first")
	old := m.stickyGen
	m, _ = m.stickyNotice("second")
	mm, _ := m.Update(stickyExpiredMsg{gen: old})
	if got := mm.(Model).statusMsg; got != "second" {
		t.Fatalf("statusMsg = %q, want the newer notice kept", got)
	}
}

// A commit bookmark whose commit is gone: the compare resolve reports it as
// the same sticky sentence, and the switcher stays.
func TestGoneCommitBookmarkCompareIsAStickyNotice(t *testing.T) {
	t.Parallel()
	m := bookmarkCopyModel(commitBookmarkFixture("b1", "a1b2c3d4e5f6a7b8", "subj"))
	mm, cmd := m.Update(entryCompareMsg{gen: m.entryCompareGen, err: &domain.CommitGoneError{SHA: "a1b2c3d4e5f6a7b8"}})
	m = mm.(Model)
	if !strings.Contains(m.statusMsg, "commit a1b2c3d is no longer available") || cmd == nil {
		t.Fatalf("statusMsg = %q cmd = %v", m.statusMsg, cmd)
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("the switcher must stay open")
	}
}
