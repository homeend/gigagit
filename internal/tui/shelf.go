package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

type shelfLoadedMsg struct {
	entries []model.ShelfEntry
	err     error
	open    bool // true → (re)open the shelf popup; false → silent refresh
	// hintBucket is true for a Task 6 hint reveal's OWN load (see
	// loadShelfForHintCmd): entries then come from the HINT's bucket, which
	// may not be the default one, so the handler must never let it
	// overwrite the general m.shelfEntries cache (that cache is meant to
	// reflect the default bucket for every OTHER consumer).
	hintBucket bool
	// gen is 0 for an ordinary load and the staging m.hintGen value for a
	// hint reveal's own load (fix F3) — see bookmarksLoadedMsg.gen's twin.
	gen int
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

// loadShelfForHintCmd is loadShelfCmd's Task 6 hint twin (ruling F1):
// ShelfFind (domain's OWN presence check, also relied on by
// domain.EvalLink) scans EVERY bucket, but ShelfList("", …) — what
// loadShelfCmd and the web's plain GET /api/shelf both call — lists the
// DEFAULT bucket only. A hint naming an entry `gg shelf add --bucket
// <name>` put anywhere else therefore resolved as present by domain and
// reported "gone" by every consumer that tried to reveal it — on the TUI
// side, answered as an outright steerFail on an entry that exists (spec
// §3.3 rule 3, "the hint degrades, it never fails", inverted).
//
// The invariant this restores: if domain resolved a hint, the consumer
// must be able to reveal it — the two must never disagree about whether an
// entry is present. Find the entry's OWN bucket first (the same call
// domain used to confirm presence), then list THAT bucket — never touching
// m.shelfEntries, which is not this bucket's list.
func (m Model) loadShelfForHintCmd(id string, tag int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		e, ferr := svc.ShelfFind(context.Background(), id)
		if ferr != nil {
			// Genuinely absent everywhere (or the shelf is disabled) — an
			// ordinary empty result, not a load ERROR: the existing
			// absent-hint branch already handles "not found in the list"
			// with the right notice, and treating this as msg.err would
			// wrongly blank the general m.shelfEntries cache.
			return shelfLoadedMsg{open: true, hintBucket: true, gen: tag}
		}
		es, err := svc.ShelfList(context.Background(), e.Bucket, 0, 0)
		return shelfLoadedMsg{entries: es, err: err, open: true, hintBucket: true, gen: tag}
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
		label: i18n.T("Add to shelf"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.shelfAddCmd(addr)
		},
	}, true
}

// shelfAddCommitCmd freezes commit sha's changed files into the shelf off the
// UI thread (reuses shelfAddedMsg). label is the human-entered name for the
// entry (from commitNamePopup); empty is fine — ShelfAddCommit/PutCommit fall
// back to a default display.
func (m Model) shelfAddCommitCmd(sha, label string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		e, err := svc.ShelfAddCommit(context.Background(), sha, label)
		return shelfAddedMsg{entry: e, err: err}
	}
}

// commitShelfRow offers "Shelf this commit" on the Commits panel — opens the
// shared naming popup, pre-filled with the commit subject, before freezing the
// selected commit's changed files durably. Mirrors commitBookmarkRow.
func (m Model) commitShelfRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	c := m.commits[bi]
	return actionRow{
		id:    "commit-shelf",
		label: i18n.T("Shelf this commit"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&commitNamePopup{commit: c, forShelf: true, name: newTextField(c.Subject)}), nil
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
	e := m.reflog[bi]
	c := model.Commit{Hash: e.Hash, Subject: e.Subject}
	return actionRow{
		id:    "reflog-shelf",
		label: i18n.T("Shelf this commit"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&commitNamePopup{commit: c, forShelf: true, name: newTextField(c.Subject)}), nil
		},
	}, true
}
