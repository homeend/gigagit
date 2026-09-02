package tui

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/i18n"
)

// f on the Branches and Worktrees tabs: jump the cursor to "where I am" —
// the checked-out branch, or the worktree gg is running in. The viewport
// follows the cursor (windowStart), so a long list scrolls to the row.
//
// Remotes is deliberately excluded: f is fetch there (canFetchRemotes), and
// the two gates are disjoint by focus so the key routes to exactly one.

func (m Model) canFindCurrent() bool {
	return (m.focus == panelBranches || m.focus == panelWorktrees) && m.panelLen(m.focus) > 0
}

// currentRowKey is the rowKeyAt identity of the focused panel's "current"
// row: the HEAD branch's name, or the served worktree's path. "" when there
// is none (detached HEAD; an unknown worktree).
func (m Model) currentRowKey(p panel) string {
	switch p {
	case panelBranches:
		for _, b := range m.branches {
			if b.IsHead {
				return b.Name
			}
		}
	case panelWorktrees:
		return m.currentWorktree
	}
	return ""
}

// findCurrentRow moves the cursor to the current row of the focused panel.
// Not moving is always explained on the status line: a detached HEAD has no
// current branch, and an active / filter can hide the row.
func (m Model) findCurrentRow() Model {
	p := m.focus
	key := m.currentRowKey(p)
	if key == "" {
		if p == panelBranches {
			m.statusMsg = i18n.T("no current branch: HEAD is detached")
		} else {
			m.statusMsg = i18n.T("current worktree unknown")
		}
		return m
	}
	for i, n := 0, m.panelLen(p); i < n; i++ {
		if sameRowKey(p, m.rowKeyAt(p, i), key) {
			m.sel[p] = i
			m.statusMsg = ""
			return m
		}
	}
	if p == panelBranches {
		m.statusMsg = i18n.T("%s is hidden by the / filter", key)
	} else {
		m.statusMsg = i18n.T("%s is hidden by the / filter", filepath.Base(key))
	}
	return m
}

// sameRowKey compares a display row's key with the wanted one. Worktree keys
// are paths: currentWorktree comes from git's top-level while the list rows
// come from `worktree list`, so compare cleaned forms rather than bytes.
func sameRowKey(p panel, got, want string) bool {
	if p == panelWorktrees {
		return filepath.Clean(got) == filepath.Clean(want)
	}
	return got == want
}
