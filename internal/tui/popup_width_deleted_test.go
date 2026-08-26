package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

// longTailPath is wider than the old fixed 56-col popup so cutoff would hide its tail.
const longTailPath = "internal/tui/some/deeply/nested/directory/structure/file.go"

// The switcher popups must widen past the old 56-col cap so long file paths are
// visible by default on a roomy terminal.
func TestShelfPopupWidensForLongPaths(t *testing.T) {
	t.Parallel()
	m := shelfPopModel(shEntry("a", longTailPath))
	m.width, m.height = 160, 40
	out := m.renderShelfPopupBox(m.shelfSwitcher())
	if !strings.Contains(out, "file.go") {
		t.Fatalf("path tail cut off at the default width:\n%s", out)
	}
	widest := 0
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > widest {
			widest = w
		}
	}
	if widest <= 58 { // old inner 56 + frame ≈ 58
		t.Fatalf("popup did not widen past the old cap (widest line = %d):\n%s", widest, out)
	}
}

// The footer hint must wrap rather than truncate, so [ctrl+w] mode is discoverable
// even on a narrow terminal (the root cause of "it has no z handling").
func TestShelfPopupFooterShowsZAtNarrowWidth(t *testing.T) {
	t.Parallel()
	m := shelfPopModel(shEntry("a", "x.go"))
	m.width, m.height = 80, 30
	out := m.renderShelfPopupBox(m.shelfSwitcher())
	if !strings.Contains(out, "[ctrl+w] mode") {
		t.Fatalf("footer must advertise [ctrl+w] mode at 80 cols (wrap, not truncate):\n%s", out)
	}
}

func TestBookmarkPopupFooterShowsZAtNarrowWidth(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(model.Bookmark{ID: "b", State: model.StateUnstaged, Worktree: "/wt", Path: "x.go"})
	m.width, m.height = 80, 30
	out := m.renderBookmarkPopupBox(m.bookmarkSwitcher())
	if !strings.Contains(out, "[ctrl+w] mode") {
		t.Fatalf("footer must advertise [ctrl+w] mode at 80 cols (wrap, not truncate):\n%s", out)
	}
}

// Even when a path is too long for the (wide) popup, scroll mode must reveal its
// tail — proving z genuinely works in these popups, not just that it is listed.
func TestShelfPopupScrollRevealsPathTail(t *testing.T) {
	t.Parallel()
	m := shelfPopModel(shEntry("a", longTailPath))
	m.width, m.height = 60, 30 // narrow box so the path overflows even widened
	p := m.shelfSwitcher()
	p.mode = modeScroll
	cutoff := m.renderShelfPopupBox(p)
	if strings.Contains(cutoff, "file.go") {
		t.Skip("path fits without scrolling at this width; nothing to reveal")
	}
	for i := 0; i < 40 && !strings.Contains(m.renderShelfPopupBox(p), "file.go"); i++ {
		p.hscroll += m.hscrollStep()
	}
	if !strings.Contains(m.renderShelfPopupBox(p), "file.go") {
		t.Fatalf("scroll mode never revealed the path tail:\n%s", m.renderShelfPopupBox(p))
	}
}

// A file deleted in a commit (status D) has no content at commit:hash:path, so
// the focused-file ref (which feeds add-to-shelf/bookmark and compare-against)
// must report "no file".
func TestDeletedCommitFileHasNoFocusedRef(t *testing.T) {
	t.Parallel()
	base := footerModel()
	base.filesHash = "abc1234"
	base.filesTreeFocused = true

	deleted := base
	deleted.filesView = &contentPopup{lines: []contentLine{{text: "x", path: "gone.go", status: "D"}}}
	if _, ok := deleted.focusedBookmark(); ok {
		t.Fatal("a D (deleted) commit-file must not be a focusable file ref")
	}
	if _, ok := deleted.shelfAddRow(); ok {
		t.Error("deleted file must not offer Add to shelf")
	}
	if _, ok := deleted.bookmarkAddRow(); ok {
		t.Error("deleted file must not offer Bookmark this file")
	}

	// A renamed file (R) lives at its new path with content — still bookmarkable.
	renamed := base
	renamed.filesView = &contentPopup{lines: []contentLine{{text: "x", path: "new.go", oldPath: "old.go", status: "R"}}}
	if _, ok := renamed.focusedBookmark(); !ok {
		t.Fatal("a renamed (R) commit-file must remain a focusable file ref")
	}
}
