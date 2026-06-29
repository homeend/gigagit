package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pressEnter and pressRuneKey drive Update with a key and return the new Model
// plus the cmd (so async loads can be run through).
func pressEnter(m Model) (Model, tea.Cmd) {
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return u.(Model), cmd
}

func pressRuneKey(m Model, r string) (Model, tea.Cmd) {
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)})
	return u.(Model), cmd
}

// enter on a real commit (files view not yet open) opens the changed-files tree
// AND lands focus on the tree — the "l + focus tree" drill-in. l, by contrast,
// opens on the commit-list side. Locking both sides keeps the delta from
// silently collapsing in a future refactor.
func TestCommitEnterOpensFilesViewFocusedOnTree(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	m, _ = pressEnter(m)
	if m.filesView == nil {
		t.Fatalf("enter on a commit should open the files view")
	}
	if !m.filesTreeFocused {
		t.Fatalf("enter should land focus on the file tree")
	}
}

func TestCommitLOpensFilesViewOnListSide(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	m, _ = pressRuneKey(m, "l")
	if m.filesView == nil {
		t.Fatalf("l on a commit should open the files view")
	}
	if m.filesTreeFocused {
		t.Fatalf("l should open on the commit-list side (tree not focused)")
	}
}

// With the files view open on the commit-list side (e.g. opened via l), enter
// drills in: it moves focus to the file tree without opening a diff. Mirrors
// enter on the Commits panel.
func TestFilesViewEnterOnListSideFocusesTree(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	m, cmd := pressRuneKey(m, "l") // open on the commit-list side
	if cmd != nil {
		u, _ := m.Update(cmd())
		m = u.(Model)
	}
	if m.filesView == nil || m.filesTreeFocused {
		t.Fatalf("setup: files view should be open on the commit-list side")
	}

	m, _ = pressEnter(m)
	if !m.filesTreeFocused {
		t.Fatalf("enter on the commit-list side should focus the tree")
	}
	if m.diffLayer() != nil {
		t.Fatalf("enter on the commit-list side must not open a diff")
	}
	if m.filesView == nil {
		t.Fatalf("the files view must stay open")
	}
}

// While the files tree is open, i shows the selected commit's message popup —
// the same action as i with no tree shown — and LAYERS OVER the tree rather
// than replacing it: filesView stays non-nil so esc returns to the tree.
func TestFilesViewIShowsCommitMessageOverTree(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	m, cmd := pressEnter(m) // open files view, focused on tree
	if cmd != nil {
		u, _ := m.Update(cmd()) // drain the changed-files load
		m = u.(Model)
	}
	if m.filesView == nil {
		t.Fatalf("setup: files view should be open")
	}

	m, cmd = pressRuneKey(m, "i")
	if cmd == nil {
		t.Fatalf("i should kick off the async message load")
	}
	cp := layerOf[*contentPopup](m)
	if cp == nil {
		t.Fatalf("i should push a commit-message popup")
	}
	// The popup must be for the commit the tree is SHOWING (filesHash), not just
	// "some popup" — lock the right-commit guarantee.
	if want := commitMessageTitle(shortHash(m.filesHash)); cp.title != want {
		t.Fatalf("popup title = %q, want %q (the displayed commit)", cp.title, want)
	}
	if m.filesView == nil {
		t.Fatalf("i must layer over the files view, not replace it")
	}
	if m.focus != panelCommits {
		t.Fatalf("focus should remain panelCommits, got %v", m.focus)
	}
}

// A reflog/tags-opened files view sets focus=panelCommits but displays a commit
// that is NOT the Commits-panel selection. i must show the DISPLAYED commit's
// message (keyed by filesHash), not the Commits cursor's.
func TestFilesViewIShowsDisplayedCommitNotCursor(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	m.sel[panelCommits] = 0 // cursor on commits[0]

	// Open a files view on the OTHER commit, as openReflogFiles/tagJumpToCommit do.
	other := m.commits[1]
	m, cmd := m.openChangedFiles(other)
	m.focus = panelCommits
	m = m.focusTree()
	if cmd != nil {
		u, _ := m.Update(cmd())
		m = u.(Model)
	}
	if m.filesHash != other.Hash {
		t.Fatalf("setup: filesHash = %q, want %q", m.filesHash, other.Hash)
	}

	m, _ = pressRuneKey(m, "i")
	cp := layerOf[*contentPopup](m)
	if cp == nil {
		t.Fatalf("i should push a commit-message popup")
	}
	if want := commitMessageTitle(shortHash(other.Hash)); cp.title != want {
		t.Fatalf("popup title = %q, want %q (displayed commit, not the cursor)", cp.title, want)
	}
}
