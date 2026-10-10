package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

type shelfLoadedMsg struct {
	slotStamp // the slot it was asked from (slot_msg.go)
	entries   []model.ShelfEntry
	err       error
	open      bool // true → (re)open the shelf popup; false → silent refresh
	// sharedDone: the entries already reached m.shelfEntries when the
	// message was queued for a sleeping slot (sharedWriter); the replay must
	// not write them again over a newer list.
	sharedDone bool
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
	slot := m.stamp()
	return func() tea.Msg {
		es, err := svc.ShelfList(context.Background(), "", 0, 0)
		return shelfLoadedMsg{slotStamp: slot, entries: es, err: err, open: open}
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
	slot := m.stamp()
	return func() tea.Msg {
		e, ferr := svc.ShelfFind(context.Background(), id)
		if ferr != nil {
			// Genuinely absent everywhere (or the shelf is disabled) — an
			// ordinary empty result, not a load ERROR: the existing
			// absent-hint branch already handles "not found in the list"
			// with the right notice, and treating this as msg.err would
			// wrongly blank the general m.shelfEntries cache.
			return shelfLoadedMsg{slotStamp: slot, open: true, hintBucket: true, gen: tag}
		}
		es, err := svc.ShelfList(context.Background(), e.Bucket, 0, 0)
		return shelfLoadedMsg{slotStamp: slot, entries: es, err: err, open: true, hintBucket: true, gen: tag}
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
	slotStamp // the worktree the file was marked in: its marks are consumed, not the viewed one's (slot_msg.go)
	entry     model.ShelfEntry
	err       error
	// unmark is the Status file-mark to drop on success ("" = the file was
	// the cursor row, not a mark): a shelved mark is consumed, like a stashed
	// one.
	unmark string
}

// shelfAddCmd freezes addr's bytes into the default bucket off the UI thread.
// unmark names the file-mark to clear once it lands ("" for none).
func (m Model) shelfAddCmd(addr model.FileAddress, unmark string) tea.Cmd {
	svc, slot := m.svc, m.stamp()
	return func() tea.Msg {
		e, err := svc.ShelfAdd(context.Background(), addr, "")
		return shelfAddedMsg{slotStamp: slot, entry: e, err: err, unmark: unmark}
	}
}

// shelfSetAddedMsg reports a marked SET shelved as one files entry. paths
// are the members' Status paths: their marks are consumed on success.
type shelfSetAddedMsg struct {
	slotStamp // the worktree the files were marked in (slot_msg.go)
	entry     model.ShelfEntry
	paths     []string
	err       error
}

// shelfAddFilesCmd freezes the addresses into ONE files entry (a tar with one
// member per file) off the UI thread — the set stays together on the shelf.
func (m Model) shelfAddFilesCmd(addrs []model.FileAddress, label string) tea.Cmd {
	svc, slot := m.svc, m.stamp()
	return func() tea.Msg {
		e, err := svc.ShelfAddFiles(context.Background(), addrs, label)
		paths := make([]string, 0, len(addrs))
		for _, a := range addrs {
			paths = append(paths, a.Path)
		}
		return shelfSetAddedMsg{slotStamp: slot, entry: e, paths: paths, err: err}
	}
}

// onBareFilesPanel reports whether the file under focus is a Files/Staged
// panel row itself — no viewer, blame, history, diff or files-view layer is
// showing a file of its own. Mirrors focusedBookmark's precedence: only then
// does the panel's marked set (m) apply.
func (m Model) onBareFilesPanel() bool {
	switch m.topLayer().(type) {
	case *fileViewer, *historyView, *blameView:
		return false
	}
	return m.diffLayer() == nil && m.filesView == nil && m.isFilesPanel(m.focus)
}

// shelfAddTargets resolves what "Add to shelf" freezes: the marked file set
// restricted to the focused panel's members when any marks exist on a bare
// Files/Staged panel (like space/stage), otherwise the single focused file.
// Reads m.status.Files directly so an active text filter never narrows the set.
// marked reports whether the set came from marks (the label says so: a single
// marked file away from the cursor would otherwise read like the cursor row).
func (m Model) shelfAddTargets() (addrs []model.FileAddress, marked bool) {
	if len(m.fileMarks) > 0 && m.onBareFilesPanel() {
		for i, f := range m.status.Files {
			if m.fileMarks[f.Path] && m.memberOf(m.focus, i) {
				addrs = append(addrs, m.panelFileBookmark(m.focus, f).Address())
			}
		}
		if len(addrs) > 0 {
			return addrs, true
		}
	}
	if addr, ok := m.focusedShelfAddress(); ok {
		return []model.FileAddress{addr}, false
	}
	return nil, false
}

// shelfAddRow is the menu-only "Add to shelf" action, present wherever a file
// is focused. With several marked files on the Files/Staged panel it shelves
// the whole set as ONE named files entry (the … says it asks for the name)
// and says so in its label. Its run handler captures the resolved addresses at
// build time.
func (m Model) shelfAddRow() (actionRow, bool) {
	addrs, marked := m.shelfAddTargets()
	if len(addrs) == 0 {
		return actionRow{}, false
	}
	if len(addrs) == 1 {
		addr := addrs[0]
		label, unmark := i18n.T("Add to shelf"), ""
		if marked {
			label, unmark = i18n.T("Add the marked file to shelf"), addr.Path
		}
		return actionRow{
			id:    "shelf-add",
			label: label,
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m, m.shelfAddCmd(addr, unmark)
			},
		}, true
	}
	return actionRow{
		id:    "shelf-add",
		label: i18n.T("Add %d marked files to shelf…", len(addrs)),
		run: func(m Model) (tea.Model, tea.Cmd) {
			// The set gets a name first (what the shelf row will say), like a
			// shelved commit; enter in the popup fires shelfAddFilesCmd.
			return m.pushLayer(&shelfSetNamePopup{addrs: addrs, name: newTextField("WIP on " + m.status.Branch)}), nil
		},
	}, true
}

// shelfAddCommitCmd freezes commit sha's changed files into the shelf off the
// UI thread (reuses shelfAddedMsg). label is the human-entered name for the
// entry (from commitNamePopup); empty is fine — ShelfAddCommit/PutCommit fall
// back to a default display.
func (m Model) shelfAddCommitCmd(sha, label string) tea.Cmd {
	svc, slot := m.svc, m.stamp()
	return func() tea.Msg {
		e, err := svc.ShelfAddCommit(context.Background(), sha, label)
		return shelfAddedMsg{slotStamp: slot, entry: e, err: err}
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

// applyShared lands the repository's shelf list at once when the message
// is queued for a sleeping slot (slot_replay.go); the popup part replays.
func (msg shelfLoadedMsg) applyShared(m Model) (Model, tea.Msg) {
	if msg.err == nil && !msg.hintBucket {
		m.shelfEntries = msg.entries
		msg.sharedDone = true
	}
	return m, msg
}
