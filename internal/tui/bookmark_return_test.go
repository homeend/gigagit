package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// switcherModel opens the bookmark switcher with one bookmark on the stack. The
// bookmark is an unstaged working-tree file backed by a real temp file so the
// paste flow's BookmarkBytes read (bookmark_popup.go) resolves and `p` opens
// the paste popup for real (committed/staged bookmarks would need a git verb).
func switcherModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "app.go"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := Model{
		svc:       domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}),
		width:     80,
		height:    24,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
	}
	bm := model.Bookmark{ID: "b1", State: model.StateUnstaged, Worktree: dir, Path: "src/app.go"}
	return m.pushOverlay(newBookmarkPopup([]model.Bookmark{bm}))
}

func TestPasteEscReturnsToSwitcher(t *testing.T) {
	m := switcherModel(t)
	m.bookmarkSwitcher().filter = "ap" // a state we expect to survive
	// open paste (p)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = u.(Model)
	if _, ok := m.overlayTop().(*bookmarkPastePopup); !ok {
		t.Fatal("p must push the paste popup on top")
	}
	// esc the paste popup
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	sw := m.bookmarkSwitcher()
	if sw == nil || m.overlayTop() != sw {
		t.Fatal("esc must return to the bookmark switcher")
	}
	if sw.filter != "ap" {
		t.Fatalf("switcher filter = %q, must survive the round trip", sw.filter)
	}
}

func TestPasteDestPrefilled(t *testing.T) {
	m := switcherModel(t)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = u.(Model)
	pp, ok := m.overlayTop().(*bookmarkPastePopup)
	if !ok {
		t.Fatal("paste popup must be open")
	}
	if pp.dest != "src/app_RESTORED.go" {
		t.Fatalf("dest = %q, want the prefilled _RESTORED path", pp.dest)
	}
}

// BLOCKER B1 regression: while an op is running the switcher sits visible on
// the overlay stack but must be INERT — a keypress must not launch a second op.
func TestSwitcherInertWhileRunning(t *testing.T) {
	m := switcherModel(t)
	m.running = true
	m.opMsgs = make(chan tea.Msg, 1) // a sentinel we must not clobber
	before := m.opMsgs
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = u.(Model)
	if cmd != nil {
		t.Fatal("a keypress while running must be a no-op (no new op/cmd)")
	}
	if m.opMsgs != before {
		t.Fatal("the running op's channel must not be replaced")
	}
	if _, ok := m.overlayTop().(*bookmarkPastePopup); ok {
		t.Fatal("p must not open the paste popup while running")
	}
}

// Remove-success refreshes the SAME switcher overlay in place (not a second
// push), dropping the deleted row.
func TestRemoveSuccessRefreshesSwitcher(t *testing.T) {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	m = m.pushOverlay(newBookmarkPopup([]model.Bookmark{{ID: "b1", Path: "a.go"}, {ID: "b2", Path: "b.go"}}))
	depth := len(m.overlays.entries)
	u, _ := m.Update(bookmarksLoadedMsg{items: []model.Bookmark{{ID: "b2", Path: "b.go"}}})
	m = u.(Model)
	if len(m.overlays.entries) != depth {
		t.Fatalf("overlay depth = %d, want %d (refresh in place, not push)", len(m.overlays.entries), depth)
	}
	sw := m.bookmarkSwitcher()
	if sw == nil || len(sw.items) != 1 || sw.items[0].ID != "b2" {
		t.Fatalf("switcher must be refreshed to the new list, got %+v", sw)
	}
}

// S4 regression: an open overlay swallows the mouse (no panel hit-test).
func TestSwitcherSwallowsMouse(t *testing.T) {
	m := switcherModel(t)
	u, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: 1})
	if cmd != nil {
		t.Fatal("mouse over an open switcher must be swallowed")
	}
	if m.bookmarkSwitcher() == nil || u.(Model).bookmarkSwitcher() == nil {
		t.Fatal("the switcher must stay open")
	}
}
