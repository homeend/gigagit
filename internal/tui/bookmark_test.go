package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/model"
)

func bmPopupModel(items ...model.Bookmark) Model {
	m := footerModel()
	m.bookmarkPopup = newBookmarkPopup(items)
	return m
}

func TestBookmarkRemoveConfirms(t *testing.T) {
	m := bmPopupModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	mm, _ := m.updateBookmarkPopupKey(keyMsg("x"))
	m = mm.(Model)
	if m.modal == nil {
		t.Fatalf("x should open a remove-confirm modal")
	}
}

func TestBookmarkPasteOpensPathPopup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := bmPopupModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: dir, Path: "a.go"})
	mm, _ := m.updateBookmarkPopupKey(keyMsg("p"))
	m = mm.(Model)
	if m.bookmarkPastePopup == nil {
		t.Fatalf("p should open the paste path popup")
	}
	if string(m.bookmarkPastePopup.data) != "payload" {
		t.Fatalf("paste popup data = %q, want payload", m.bookmarkPastePopup.data)
	}
}

func TestBookmarkCompareTwoOpensDiff(t *testing.T) {
	m := bmPopupModel(
		model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"},
		model.Bookmark{ID: "b", State: model.StateUnstaged, Worktree: "/wt", Path: "b.go"},
	)
	m, _ = m.openBookmarkCompareTwo("a", "b")
	if m.diffView == nil || m.diffTag != "bookmark2:a:b" {
		t.Fatalf("two-bookmark compare should open a diff (tag=%q)", m.diffTag)
	}
}

func TestBookmarkFilterModeTypesNotActs(t *testing.T) {
	m := bmPopupModel(
		model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "app.go"},
		model.Bookmark{ID: "b", State: model.StateUnstaged, Worktree: "/wt", Path: "readme.md"},
	)
	mm, _ := m.updateBookmarkPopupKey(keyMsg("/")) // enter filter mode
	m = mm.(Model)
	if !m.bookmarkPopup.filtering {
		t.Fatalf("/ should enter filter mode")
	}
	mm, _ = m.updateBookmarkPopupKey(keyMsg("p")) // a path char, not the paste action
	m = mm.(Model)
	if m.bookmarkPastePopup != nil {
		t.Fatalf("p while filtering must type, not trigger paste")
	}
	if m.bookmarkPopup == nil || m.bookmarkPopup.filter != "p" {
		t.Fatalf("filter should be 'p', got %q", m.bookmarkPopup.filter)
	}
}

func TestBookmarkPasteEnterStartsWrite(t *testing.T) {
	m := footerModel()
	m.bookmarkPastePopup = &bookmarkPastePopup{origin: "a.go", data: []byte("x")}
	// Empty dest is a no-op (popup stays open).
	mm, _ := m.updateBookmarkPasteKey(keyMsg("enter"))
	m = mm.(Model)
	if m.bookmarkPastePopup == nil {
		t.Fatalf("empty dest should keep the popup open")
	}
	for _, r := range "out.txt" {
		mm, _ = m.updateBookmarkPasteKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	mm, cmd := m.updateBookmarkPasteKey(keyMsg("enter"))
	m = mm.(Model)
	if m.bookmarkPastePopup != nil {
		t.Fatalf("enter with a dest should close the popup")
	}
	if cmd == nil {
		t.Fatalf("enter with a dest should start the write op")
	}
}

func TestBookmarkMarkThenCompare(t *testing.T) {
	m := bmPopupModel(
		model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"},
		model.Bookmark{ID: "b", State: model.StateUnstaged, Worktree: "/wt", Path: "b.go"},
	)
	mm, _ := m.updateBookmarkPopupKey(keyMsg("m")) // mark row 0
	m = mm.(Model)
	if m.bookmarkPopup == nil || m.bookmarkPopup.markID != "a" {
		t.Fatalf("first m should mark the cursor bookmark")
	}
	m.bookmarkPopup.sel = 1
	mm, _ = m.updateBookmarkPopupKey(keyMsg("m")) // compare with row 1
	m = mm.(Model)
	if m.diffView == nil {
		t.Fatalf("second m on another row should open the compare diff")
	}
}

func TestBookmarkDisplayString(t *testing.T) {
	got := bookmarkDisplay(model.Bookmark{State: model.StateCommitted, Commit: "a1b2c3d4e5", Path: "src/x.go", Branch: "feat"})
	if !strings.Contains(got, "src/x.go") || !strings.Contains(got, "a1b2c3d") || !strings.Contains(got, "feat") {
		t.Fatalf("display = %q", got)
	}
}

// Full path: g opens (async load), the loaded msg builds the popup, View renders it.
func TestBookmarkPopupOpenAndRenderFullPath(t *testing.T) {
	m := footerModel()
	m.width, m.height = 100, 30
	u, cmd := m.Update(keyMsg("g"))
	m = u.(Model)
	if cmd == nil {
		t.Fatal("g should fire a bookmark-load command")
	}
	u, _ = m.Update(bookmarksLoadedMsg{items: []model.Bookmark{
		{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a/b.go"},
	}})
	m = u.(Model)
	if m.bookmarkPopup == nil {
		t.Fatal("loaded msg should open the popup")
	}
	out := m.View()
	if !strings.Contains(out, "a/b.go") {
		t.Fatalf("popup not rendered:\n%s", out)
	}
}

func TestBookmarkRowOnFilesPanel(t *testing.T) {
	m := filesMenuModel() // panelFiles focused with one tracked file "dir/f.txt"
	m.currentWorktree = "/wt"
	if _, ok := findRow(availableActions(m), "bookmark-add"); !ok {
		t.Fatalf("Bookmark this file missing on Files panel")
	}
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateUnstaged || b.Worktree != "/wt" || b.Path != "dir/f.txt" {
		t.Fatalf("focusedBookmark = %+v ok=%v", b, ok)
	}
}

func TestBookmarkRowAbsentWhenNoFile(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.focusedBookmark(); ok {
		t.Fatalf("no file focused → focusedBookmark should be false")
	}
}

func TestBookmarkPopupWindowsLongList(t *testing.T) {
	var items []model.Bookmark
	for i := 0; i < 30; i++ {
		items = append(items, model.Bookmark{
			ID: fmt.Sprintf("b%02d", i), State: model.StateUnstaged,
			Worktree: "/wt", Path: fmt.Sprintf("f%02d.go", i),
		})
	}
	m := bmPopupModel(items...)
	m.width, m.height = 80, 30
	m.bookmarkPopup.sel = 29 // selection at the bottom
	out := m.renderBookmarkPopup()
	if !strings.Contains(out, "f29.go") {
		t.Fatalf("selected (bottom) row must be visible:\n%s", out)
	}
	if strings.Contains(out, "f00.go") {
		t.Fatalf("top row must scroll out of the capped viewport:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Fatalf("line wider than terminal (%d > %d): %q", w, m.width, line)
		}
	}
}

func TestBookmarkPopupZCyclesMode(t *testing.T) {
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	m.bookmarkPopup.hscroll = 5
	mm, _ := m.updateBookmarkPopupKey(keyMsg("z"))
	m = mm.(Model)
	if m.bookmarkPopup.mode != modeWrap {
		t.Fatalf("z should cycle cutoff→wrap, got %v", m.bookmarkPopup.mode)
	}
	if m.bookmarkPopup.hscroll != 0 {
		t.Fatalf("z should reset hscroll, got %d", m.bookmarkPopup.hscroll)
	}
	mm, _ = m.updateBookmarkPopupKey(keyMsg("z"))
	m = mm.(Model)
	if m.bookmarkPopup.mode != modeScroll {
		t.Fatalf("second z should reach scroll, got %v", m.bookmarkPopup.mode)
	}
}

func TestBookmarkPopupPanOnlyInScroll(t *testing.T) {
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	// cutoff (default): shift+right is a no-op
	mm, _ := m.updateBookmarkPopupKey(keyMsg("shift+right"))
	m = mm.(Model)
	if m.bookmarkPopup.hscroll != 0 {
		t.Fatalf("shift+right in cutoff must not pan, got %d", m.bookmarkPopup.hscroll)
	}
	// scroll mode: shift+right pans by one step, shift+left clamps at 0
	m.bookmarkPopup.mode = modeScroll
	mm, _ = m.updateBookmarkPopupKey(keyMsg("shift+right"))
	m = mm.(Model)
	if m.bookmarkPopup.hscroll != m.hscrollStep() {
		t.Fatalf("shift+right in scroll → hscroll=%d, want %d", m.bookmarkPopup.hscroll, m.hscrollStep())
	}
	mm, _ = m.updateBookmarkPopupKey(keyMsg("shift+left"))
	m = mm.(Model)
	if m.bookmarkPopup.hscroll != 0 {
		t.Fatalf("shift+left should clamp to 0, got %d", m.bookmarkPopup.hscroll)
	}
}

func TestBookmarkPopupZTypesWhileFiltering(t *testing.T) {
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "zebra.go"})
	mm, _ := m.updateBookmarkPopupKey(keyMsg("/")) // enter filter mode
	m = mm.(Model)
	mm, _ = m.updateBookmarkPopupKey(keyMsg("z")) // a query char, not a mode cycle
	m = mm.(Model)
	if m.bookmarkPopup.mode != modeCutoff {
		t.Fatalf("z while filtering must not cycle mode, got %v", m.bookmarkPopup.mode)
	}
	if m.bookmarkPopup.filter != "z" {
		t.Fatalf("z while filtering should type, filter=%q", m.bookmarkPopup.filter)
	}
}
