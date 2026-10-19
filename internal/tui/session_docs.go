package tui

// The TUI as a session-docs READER (spec 2026-10-18-gg-session-docs §4,
// §6.4): every worktree it shows — the home one and every view slot — is
// attached to that worktree's docs directory, under the same session dir
// the steer inbox lives in. Attach reloads synchronously, so the rows are
// there the moment a slot shows. Detach only when a slot is pruned or the
// repository changes; a swap keeps its documents.

import (
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// docsDirFor is worktree's session-docs directory, "" while the session dir
// is not resolved yet (the snapshot target lands it) — then nothing attaches.
func (m Model) docsDirFor(worktree string) string {
	if m.docsDirOf != nil {
		return m.docsDirOf(worktree) // tests
	}
	return steer.DocsDir(steerDirFor(m.snapshotCommonDir, worktree))
}

// attachDocs binds worktree's root to its docs directory. A directory that
// cannot be made or read attaches nothing and says nothing: the TUI runs
// without session rows.
func (m Model) attachDocs(worktree string) {
	dir := m.docsDirFor(worktree)
	if dir == "" || worktree == "" || m.docs == nil {
		return
	}
	_ = m.docs.Attach(domain.CheckoutKey(worktree), worktree, dir)
}

// attachShownRoots attaches the home worktree and every slot, then lists
// what their directories hold.
func (m Model) attachShownRoots() Model {
	for _, v := range m.views {
		m.attachDocs(v.path)
	}
	m.attachDocs(m.currentWorktree)
	return m.syncOverviews()
}

// detachDocs forgets worktree's documents (a pruned slot, a repo switch).
func (m Model) detachDocs(worktree string) {
	if m.docs != nil && worktree != "" {
		m.docs.Detach(domain.CheckoutKey(worktree))
	}
}

// detachShownRoots is detachDocs for every slot and the current worktree.
func (m Model) detachShownRoots() {
	for _, v := range m.views {
		m.detachDocs(v.path)
	}
	m.detachDocs(m.currentWorktree)
}
