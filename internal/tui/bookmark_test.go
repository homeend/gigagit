package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

func bmPopupModel(items ...model.Bookmark) Model {
	m := footerModel()
	m = m.pushLayer(newBookmarkPopup(items))
	return m
}

// TestBookmarkArrowsMoveWhileTyping proves the shared filter-motion contract
// reaches the bookmark switcher too: arrows move the selection while typing the
// `/` filter, without dropping the query (same as the commit filter and finder).
func TestBookmarkArrowsMoveWhileTyping(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(
		model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "alpha.go"},
		model.Bookmark{ID: "b2", State: model.StateUnstaged, Worktree: "/wt", Path: "alpine.go"},
		model.Bookmark{ID: "b3", State: model.StateUnstaged, Worktree: "/wt", Path: "alto.go"},
	)
	mm, _ := m.Update(keyMsg("/"))
	m = mm.(Model)
	for _, r := range "al" { // matches all three
		mm, _ = m.Update(keyMsg(string(r)))
		m = mm.(Model)
	}
	p := layerOf[*bookmarkPopup](m)
	if !p.filtering || p.filter != "al" || len(p.visibleIdx()) < 2 {
		t.Fatalf("setup: filtering=%v filter=%q visible=%d", p.filtering, p.filter, len(p.visibleIdx()))
	}
	mm, _ = m.Update(keyMsg("down"))
	m = mm.(Model)
	p = layerOf[*bookmarkPopup](m)
	if p.sel != 1 || p.filter != "al" {
		t.Fatalf("down should move selection while typing; sel=%d filter=%q", p.sel, p.filter)
	}
}

func TestBookmarkRemoveConfirms(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	mm, _ := m.Update(keyMsg("x"))
	m = mm.(Model)
	if m.modal == nil {
		t.Fatalf("x should open a remove-confirm modal")
	}
}

func TestBookmarkPasteOpensPathPopup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := bmPopupModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: dir, Path: "a.go"})
	mm, _ := m.Update(keyMsg("p"))
	m = mm.(Model)
	if bookmarkPasteOf(m) == nil {
		t.Fatalf("p should open the paste path popup")
	}
	if string(bookmarkPasteOf(m).data) != "payload" {
		t.Fatalf("paste popup data = %q, want payload", bookmarkPasteOf(m).data)
	}
}

func TestBookmarkCompareTwoOpensDiff(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(
		model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"},
		model.Bookmark{ID: "b", State: model.StateUnstaged, Worktree: "/wt", Path: "b.go"},
	)
	m, _ = m.openBookmarkCompareTwo("a", "b")
	if m.diffLayer() == nil || m.diffTag != "bookmark2:a:b" {
		t.Fatalf("two-bookmark compare should open a diff (tag=%q)", m.diffTag)
	}
}

func TestBookmarkFilterModeTypesNotActs(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(
		model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "app.go"},
		model.Bookmark{ID: "b", State: model.StateUnstaged, Worktree: "/wt", Path: "readme.md"},
	)
	mm, _ := m.Update(keyMsg("/")) // enter filter mode
	m = mm.(Model)
	if !m.bookmarkSwitcher().filtering {
		t.Fatalf("/ should enter filter mode")
	}
	mm, _ = m.Update(keyMsg("p")) // a path char, not the paste action
	m = mm.(Model)
	if bookmarkPasteOf(m) != nil {
		t.Fatalf("p while filtering must type, not trigger paste")
	}
	if m.bookmarkSwitcher() == nil || m.bookmarkSwitcher().filter != "p" {
		t.Fatalf("filter should be 'p', got %q", m.bookmarkSwitcher().filter)
	}
}

func TestBookmarkPasteEnterStartsWrite(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(&bookmarkPastePopup{origin: "a.go", data: []byte("x")})
	// Empty dest is a no-op (popup stays open).
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if bookmarkPasteOf(m) == nil {
		t.Fatalf("empty dest should keep the popup open")
	}
	for _, r := range "out.txt" {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	mm, cmd := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if bookmarkPasteOf(m) != nil {
		t.Fatalf("enter with a dest should close the popup")
	}
	if cmd == nil {
		t.Fatalf("enter with a dest should start the write op")
	}
}

func TestBookmarkMarkThenCompare(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(
		model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"},
		model.Bookmark{ID: "b", State: model.StateUnstaged, Worktree: "/wt", Path: "b.go"},
	)
	mm, _ := m.Update(keyMsg("m")) // mark row 0
	m = mm.(Model)
	if m.bookmarkSwitcher() == nil || m.bookmarkSwitcher().markID != "a" {
		t.Fatalf("first m should mark the cursor bookmark")
	}
	m.bookmarkSwitcher().sel = 1
	mm, _ = m.Update(keyMsg("m")) // compare with row 1
	m = mm.(Model)
	if m.diffLayer() == nil {
		t.Fatalf("second m on another row should open the compare diff")
	}
}

func TestBookmarkDisplayString(t *testing.T) {
	t.Parallel()
	got := bookmarkDisplay(model.Bookmark{State: model.StateCommitted, Commit: "a1b2c3d4e5", Path: "src/x.go", Branch: "feat"})
	if !strings.Contains(got, "src/x.go") || !strings.Contains(got, "a1b2c3d") || !strings.Contains(got, "feat") {
		t.Fatalf("display = %q", got)
	}
}

// Full path: g opens (async load), the loaded msg builds the popup, View renders it.
func TestBookmarkPopupOpenAndRenderFullPath(t *testing.T) {
	t.Parallel()
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
	if m.bookmarkSwitcher() == nil {
		t.Fatal("loaded msg should open the popup")
	}
	out := m.View()
	if !strings.Contains(out, "a/b.go") {
		t.Fatalf("popup not rendered:\n%s", out)
	}
}

func TestBookmarkRowOnFilesPanel(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	m := footerModel()
	m.focus = panelBranches
	if _, ok := m.focusedBookmark(); ok {
		t.Fatalf("no file focused → focusedBookmark should be false")
	}
}

func TestBookmarkPopupWindowsLongList(t *testing.T) {
	t.Parallel()
	var items []model.Bookmark
	for i := 0; i < 30; i++ {
		items = append(items, model.Bookmark{
			ID: fmt.Sprintf("b%02d", i), State: model.StateUnstaged,
			Worktree: "/wt", Path: fmt.Sprintf("f%02d.go", i),
		})
	}
	m := bmPopupModel(items...)
	m.width, m.height = 80, 30
	m.bookmarkSwitcher().sel = 29 // selection at the bottom
	out := m.renderBookmarkPopupBox(m.bookmarkSwitcher())
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
	t.Parallel()
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	m.bookmarkSwitcher().hscroll = 5
	mm, _ := m.Update(keyMsg("ctrl+w"))
	m = mm.(Model)
	if m.bookmarkSwitcher().mode != modeWrap {
		t.Fatalf("z should cycle cutoff→wrap, got %v", m.bookmarkSwitcher().mode)
	}
	if m.bookmarkSwitcher().hscroll != 0 {
		t.Fatalf("z should reset hscroll, got %d", m.bookmarkSwitcher().hscroll)
	}
	mm, _ = m.Update(keyMsg("ctrl+w"))
	m = mm.(Model)
	if m.bookmarkSwitcher().mode != modeScroll {
		t.Fatalf("second z should reach scroll, got %v", m.bookmarkSwitcher().mode)
	}
}

func TestBookmarkPopupPanOnlyInScroll(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	// cutoff (default): shift+right is a no-op
	mm, _ := m.Update(keyMsg("shift+right"))
	m = mm.(Model)
	if m.bookmarkSwitcher().hscroll != 0 {
		t.Fatalf("shift+right in cutoff must not pan, got %d", m.bookmarkSwitcher().hscroll)
	}
	// scroll mode: shift+right pans by one step, shift+left clamps at 0
	m.bookmarkSwitcher().mode = modeScroll
	mm, _ = m.Update(keyMsg("shift+right"))
	m = mm.(Model)
	if m.bookmarkSwitcher().hscroll != m.hscrollStep() {
		t.Fatalf("shift+right in scroll → hscroll=%d, want %d", m.bookmarkSwitcher().hscroll, m.hscrollStep())
	}
	mm, _ = m.Update(keyMsg("shift+left"))
	m = mm.(Model)
	if m.bookmarkSwitcher().hscroll != 0 {
		t.Fatalf("shift+left should clamp to 0, got %d", m.bookmarkSwitcher().hscroll)
	}
}

func TestBookmarkPopupZTypesWhileFiltering(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(model.Bookmark{ID: "a", State: model.StateUnstaged, Worktree: "/wt", Path: "zebra.go"})
	mm, _ := m.Update(keyMsg("/")) // enter filter mode
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("z")) // a query char, not a mode cycle
	m = mm.(Model)
	if m.bookmarkSwitcher().mode != modeCutoff {
		t.Fatalf("z while filtering must not cycle mode, got %v", m.bookmarkSwitcher().mode)
	}
	if m.bookmarkSwitcher().filter != "z" {
		t.Fatalf("z while filtering should type, filter=%q", m.bookmarkSwitcher().filter)
	}
}

func TestFocusedBookmarkHistoryUsesSelectedRow(t *testing.T) {
	t.Parallel()
	m := footerModel()
	h := newHistoryView(navContext{path: "old.go", rev: "starthash"})
	h.commits = []model.FileCommit{
		{Commit: model.Commit{Hash: "aaaa1111"}, Path: "old.go"},
		{Commit: model.Commit{Hash: "bbbb2222"}, Path: "renamed.go"},
	}
	h.sel = 1
	m = m.pushLayer(h)
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateCommitted || b.Commit != "bbbb2222" || b.Path != "renamed.go" {
		t.Fatalf("history focusedBookmark = %+v ok=%v; want committed bbbb2222 renamed.go", b, ok)
	}
}

func TestFocusedBookmarkDiffViewCommit(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(&diffView{title: "dir/a.go", rev: "cafe9999"})
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateCommitted || b.Commit != "cafe9999" || b.Path != "dir/a.go" {
		t.Fatalf("diff focusedBookmark = %+v ok=%v; want committed cafe9999 dir/a.go", b, ok)
	}
}

func TestFocusedBookmarkDiffViewWorkingTree(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m = m.pushLayer(&diffView{title: "a.go", rev: ""}) // working-tree diff
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateUnstaged || b.Path != "a.go" {
		t.Fatalf("working-tree diff focusedBookmark = %+v ok=%v; want unstaged a.go", b, ok)
	}
}

func TestFocusedBookmarkBlameWorkingTree(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.currentWorktree = "/wt"
	m.status.Branch = "main"
	m = m.pushLayer(&blameView{ctx: navContext{path: "a.go", rev: ""}}) // working-tree blame
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateUnstaged || b.Worktree != "/wt" || b.Branch != "main" || b.Path != "a.go" {
		t.Fatalf("working-tree blame focusedBookmark = %+v ok=%v; want unstaged a.go @ /wt", b, ok)
	}
}

func TestFocusedBookmarkBlameCommitted(t *testing.T) {
	t.Parallel()
	m := footerModel().pushLayer(&blameView{ctx: navContext{path: "a.go", rev: "abc1234def"}})
	b, ok := m.focusedBookmark()
	if !ok || b.State != model.StateCommitted || b.Commit != "abc1234def" || b.Path != "a.go" {
		t.Fatalf("committed blame focusedBookmark = %+v ok=%v; want committed abc1234def a.go", b, ok)
	}
}

func TestBookmarkToFileRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   model.Bookmark
		want model.FileRef
	}{
		{"committed", model.Bookmark{State: model.StateCommitted, Commit: "deadbeef", Path: "a.go"},
			model.FileRef{Source: model.SourceCommit, Locator: "deadbeef", Path: "a.go"}},
		{"shelf", model.Bookmark{State: model.StateShelf, ShelfID: "sh1", Path: "b.go"},
			model.FileRef{Source: model.SourceShelf, Locator: "sh1", Path: "b.go"}},
		{"staged", model.Bookmark{State: model.StateStaged, Path: "c.go"},
			model.FileRef{Source: model.SourceStaged, Path: "c.go"}},
		{"unstaged", model.Bookmark{State: model.StateUnstaged, Path: "d.go"},
			model.FileRef{Source: model.SourceUnstaged, Path: "d.go"}},
		{"untracked", model.Bookmark{State: model.StateUntracked, Path: "e.go"},
			model.FileRef{Source: model.SourceUnstaged, Path: "e.go"}},
	}
	for _, c := range cases {
		if got := bookmarkToFileRef(c.in); got != c.want {
			t.Errorf("%s: bookmarkToFileRef = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestBookmarkPopupCAgainstShelf(t *testing.T) {
	t.Parallel()
	m := bmPopupModel(model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: "/wt", Path: "a.go"})
	mm, cmd := m.Update(keyMsg("c"))
	m = mm.(Model)
	if m.bookmarkSwitcher() == nil {
		t.Fatalf("c should keep the bookmark switcher on the stack (the shelf picker stacks on top)")
	}
	if m.pendingCompare == nil || m.pendingCompare.target != compareShelf {
		t.Fatalf("c should set a shelf-targeted pendingCompare, got %+v", m.pendingCompare)
	}
	if cmd == nil {
		t.Fatalf("c should load the shelf")
	}
}
