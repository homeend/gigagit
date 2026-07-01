package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

type shelfLoadedMsg struct {
	entries []model.ShelfEntry
	err     error
	open    bool // true → (re)open the shelf popup; false → silent refresh
}

// loadShelfCmd loads the default bucket's entries (all of them — the shelf is a
// local read; the Store API stays paged for a future backend). A disabled shelf
// (no state dir) yields an empty list, not an error modal. open marks loads that
// should (re)open the popup, so a stray refresh can't pop it open.
func (m Model) loadShelfCmd(open bool) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		es, err := svc.ShelfList(context.Background(), "", 0, 0)
		return shelfLoadedMsg{entries: es, err: err, open: open}
	}
}

// focusedShelfAddress resolves the file under focus to a FileAddress, reusing
// the bookmark capture (same surfaces/precedence). A shelf entry can't be
// re-shelved, so StateShelf is rejected.
func (m Model) focusedShelfAddress() (model.FileAddress, bool) {
	b, ok := m.focusedBookmark()
	if !ok || b.State == model.StateShelf {
		return model.FileAddress{}, false
	}
	return b.Address(), true
}

type shelfAddedMsg struct {
	entry model.ShelfEntry
	err   error
}

// shelfAddCmd freezes addr's bytes into the default bucket off the UI thread.
func (m Model) shelfAddCmd(addr model.FileAddress) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		e, err := svc.ShelfAdd(context.Background(), addr, "")
		return shelfAddedMsg{entry: e, err: err}
	}
}

// shelfAddRow is the menu-only "Add to shelf" action, present wherever a single
// file is focused. Its run handler captures the resolved address at build time.
func (m Model) shelfAddRow() (actionRow, bool) {
	addr, ok := m.focusedShelfAddress()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "shelf-add",
		label: "Add to shelf",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.shelfAddCmd(addr)
		},
	}, true
}

// shelfAddCommitCmd freezes commit sha's changed files into the shelf off the
// UI thread (reuses shelfAddedMsg).
func (m Model) shelfAddCommitCmd(sha string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		e, err := svc.ShelfAddCommit(context.Background(), sha)
		return shelfAddedMsg{entry: e, err: err}
	}
}

// commitShelfRow offers "Shelf this commit" on the Commits panel — freeze the
// selected commit's changed files durably. Mirrors commitBookmarkRow.
func (m Model) commitShelfRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	sha := m.commits[bi].Hash
	return actionRow{
		id:    "commit-shelf",
		label: "Shelf this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.shelfAddCommitCmd(sha)
		},
	}, true
}

// reflogShelfRow is the reflog-panel variant of commitShelfRow.
func (m Model) reflogShelfRow() (actionRow, bool) {
	if m.focus != panelReflog || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelReflog)
	if !ok {
		return actionRow{}, false
	}
	sha := m.reflog[bi].Hash
	return actionRow{
		id:    "reflog-shelf",
		label: "Shelf this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.shelfAddCommitCmd(sha)
		},
	}, true
}
