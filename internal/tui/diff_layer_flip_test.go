package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/textdiff"
)

// esc from a picker-opened compare diff returns to the picker beneath it.
func TestEscFromPickerDiffReturnsToPicker(t *testing.T) {
	m := loadedModelLinearCommits(t, 2) // existing helper; a repo with ≥2 commits
	// Push the bookmark switcher directly (openBookmarkSwitcher returns a cmd,
	// not the switcher itself — the switcher opens on bookmarksLoadedMsg).
	m = m.pushLayer(newBookmarkPopup(nil))
	if m.bookmarkSwitcher() == nil {
		t.Fatal("precondition: bookmark switcher should be open")
	}
	// Open a compare diff from the switcher (pushes the diff over the switcher).
	v := &diffView{title: "a ↔ b", loading: true}
	m, _ = m.openPickerDiff(v, "test:diff", nil)
	if m.diffLayer() == nil {
		t.Fatal("diff should be on the stack after openPickerDiff")
	}
	// esc closes the diff and reveals the switcher, still live.
	m2, _ := m.diffLayer().update(m, tea.KeyMsg{Type: tea.KeyEsc})
	m = m2
	if m.diffLayer() != nil {
		t.Fatal("esc should pop the diff")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("esc from a picker diff must return to the picker, not base")
	}
}

// diffMsg populates the on-stack diff in place even when it is not the top layer.
func TestDiffMsgPopulatesInPlaceUnderOverlay(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	v := &diffView{title: "f", loading: true}
	m, _ = m.openPickerDiff(v, "tag1", nil)
	// Push a bookmark popup OVER the loading diff.
	m = m.pushLayer(newBookmarkPopup(nil))
	loaded := &diffView{title: "f", loading: true, lines: []textdiff.Line{{}}}
	nm, _ := m.Update(diffMsg{tag: "tag1", view: loaded})
	m = nm.(Model)
	dv := m.diffLayer()
	if dv == nil || dv.loading {
		t.Fatal("diffMsg must clear loading on the on-stack diff")
	}
	if m.bookmarkSwitcher() == nil {
		t.Fatal("the overlay popup must remain on top after diffMsg")
	}
}
