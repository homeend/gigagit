package tui

import "github.com/gigagit/gg/internal/model"

// Availability predicates shared by Update's key dispatch (model.go) and the
// footer binding registry (footer.go). Sharing them keeps the footer honest:
// a key is advertised only through the same check that gates its handler.
// Footer bindings may add a focus check on top (stricter is fine); they must
// never be looser than the Update gate.

// opsIdle reports whether a new operation may start: nothing running and the
// initial load finished.
func (m Model) opsIdle() bool {
	return !m.running && !m.loading
}

// selectedBranch resolves the Branches panel selection through the view
// transforms. ok is false when the visible list is empty.
func (m Model) selectedBranch() (model.Branch, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return model.Branch{}, false
	}
	return m.branches[bi], true
}

// selectedWorktree resolves the Worktrees panel selection.
func (m Model) selectedWorktree() (model.Worktree, bool) {
	bi, ok := m.backingIndex(panelWorktrees)
	if !ok {
		return model.Worktree{}, false
	}
	return m.worktrees[bi], true
}

// canSwitchBranch gates s: SmartSwitch to the selected branch. Switching to
// the branch already checked out in this worktree: git refuses, so skip it.
func (m Model) canSwitchBranch() bool {
	b, ok := m.selectedBranch()
	return m.opsIdle() && ok && !b.IsHead
}

// canOpenBranchPopup gates b/B: a new branch from the selected one.
func (m Model) canOpenBranchPopup() bool {
	_, ok := m.selectedBranch()
	return m.opsIdle() && ok
}

// canOpenWorktreePopup gates w/W: a worktree from the selected branch. w/W
// act on the Branches selection from any focused panel; focus is not a gate.
func (m Model) canOpenWorktreePopup() bool {
	_, ok := m.selectedBranch()
	return m.opsIdle() && ok
}

// canDeleteBranch gates d on Branches: git refuses deleting the checked-out
// branch, so don't offer it.
func (m Model) canDeleteBranch() bool {
	b, ok := m.selectedBranch()
	return m.opsIdle() && ok && !b.IsHead
}

// canDeleteWorktree gates d on Worktrees: git refuses removing the current
// working tree, so don't offer it.
func (m Model) canDeleteWorktree() bool {
	wt, ok := m.selectedWorktree()
	return m.opsIdle() && ok && wt.Path != m.currentWorktree
}

// canEnterWorktree gates enter on Worktrees: re-root into another worktree.
func (m Model) canEnterWorktree() bool {
	wt, ok := m.selectedWorktree()
	return m.opsIdle() && ok && wt.Path != "" && wt.Path != m.currentWorktree
}

// canShowCommitFiles gates l: the commit files view needs a resolvable
// commit row. The narrow-terminal refusal stays in the dispatch (it keeps
// an explanatory statusMsg); the footer binding adds it as a stricter check.
func (m Model) canShowCommitFiles() bool {
	_, ok := m.backingIndex(panelCommits)
	return m.opsIdle() && ok
}

// canMark gates m: mark/unmark/pair needs a resolvable row in the focused
// panel (handleMarkKey re-checks and routes the three sub-cases).
func (m Model) canMark() bool {
	_, ok := m.backingIndex(m.focus)
	return m.opsIdle() && ok
}

// markOnFocusedPanel reports a live mark belonging to the focused panel.
func (m Model) markOnFocusedPanel() bool {
	return m.mark != nil && m.mark.panel == m.focus && m.markAlive()
}

// cursorOnMark reports whether the focused panel's selection is the marked row.
func (m Model) cursorOnMark() bool {
	if m.mark == nil {
		return false
	}
	bi, ok := m.backingIndex(m.focus)
	return ok && m.listFor(m.focus).Key(bi) == m.mark.key
}

// canCommit reports whether there is a staged index to commit and no op is running.
func (m Model) canCommit() bool {
	return m.opsIdle() && m.status.Counts().Staged > 0
}

// canAmend reports whether HEAD has a commit to amend and no op is running.
func (m Model) canAmend() bool {
	return m.opsIdle() && len(m.commits) > 0
}

// canStage reports whether the selected Status row can be staged/unstaged:
// the Status panel is focused, a row is selected, and no op is running.
func (m Model) canStage() bool {
	if m.focus != panelFiles || !m.opsIdle() {
		return false
	}
	_, ok := m.backingIndex(panelFiles)
	return ok
}

// canShowFileDiff gates enter on Status: the side-by-side diff of the
// selected file. Conflicted rows are excluded until the conflict editor
// exists. The width check uses the !(w>0 && w<60) idiom so a model that has
// not seen a WindowSizeMsg yet (tests) is not refused.
func (m Model) canShowFileDiff() bool {
	bi, ok := m.backingIndex(panelFiles)
	if !ok {
		return false
	}
	f := m.status.Files[bi]
	return m.opsIdle() && f.Kind != model.KindUnmerged && !(m.width > 0 && m.width < 60)
}
