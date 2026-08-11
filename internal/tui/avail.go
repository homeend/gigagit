package tui

import "github.com/homeend/gigagit/internal/model"

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

// paletteReachable reports whether ctrl+p may open the command palette right now.
// It opens over exactly the surfaces the other global keys (g/G/F) already open
// over: the base panels and the read-only browse windows — the files-tree and
// stash-list field surfaces (topLayer is nil for those), and the diff/history/
// blame layers. It never opens while an input popup, an interactive editor
// (interactive rebase / hunk staging), a decision modal, a process, or the
// action menu owns the keyboard, and only when ops are idle. (The review viewer
// is excluded to stay in lock-step with g/G/F, which it also doesn't wire.)
func (m Model) paletteReachable() bool {
	if !m.opsIdle() || m.modal != nil || m.proc != nil || m.actionMenu != nil {
		return false
	}
	switch m.topLayer().(type) {
	case nil, *diffView, *historyView, *blameView:
		return true
	default:
		// popups, the palette itself, the review viewer, interactive editors
		// (irebaseEditor/hunkPicker), …
		return false
	}
}

// canStageHunks reports whether the Files panel's selected row is a tracked,
// non-conflicted file the hunk-staging picker can open.
func (m Model) canStageHunks() bool {
	if m.focus != panelFiles || !m.opsIdle() {
		return false
	}
	bi, ok := m.backingIndex(panelFiles)
	if !ok {
		return false
	}
	f := m.status.Files[bi]
	return f.Kind != model.KindUntracked && f.Kind != model.KindUnmerged
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

// selectedRemote resolves the Remotes panel selection through the view
// transforms. ok is false when the visible list is empty.
func (m Model) selectedRemote() (model.RemoteBranch, bool) {
	bi, ok := m.backingIndex(panelRemotes)
	if !ok {
		return model.RemoteBranch{}, false
	}
	return m.remoteBranches[bi], true
}

// canCheckoutRemote gates c/s on the Remotes tab: a remote row is selected and
// no op is running.
func (m Model) canCheckoutRemote() bool {
	_, ok := m.selectedRemote()
	return m.opsIdle() && ok
}

// selectedRemoteForAction is selectedRemote with the call-site context gate the
// keyboard/footer paths apply: it resolves ONLY when the Remotes tab is the
// focused panel and no op is running. The . menu remote-branch rows must use
// this rather than bare selectedRemote() — which is focus-agnostic (it reads the
// Remotes panel's stored selection even when another left tab is active), so
// using it directly leaked those rows onto the Branches/Worktrees tabs.
func (m Model) selectedRemoteForAction() (model.RemoteBranch, bool) {
	if m.focus != panelRemotes || !m.opsIdle() {
		return model.RemoteBranch{}, false
	}
	return m.selectedRemote()
}

// worktreeForBranch returns a loaded worktree other than the current one that
// has branch checked out, if any — the case where SmartSwitch would fail
// because git refuses to check a branch out in two worktrees at once.
func (m Model) worktreeForBranch(branch string) (model.Worktree, bool) {
	for _, w := range m.worktrees {
		if w.Branch == branch && w.Path != m.currentWorktree {
			return w, true
		}
	}
	return model.Worktree{}, false
}

// worktreeAbsPathForBranch returns the absolute path of the worktree that has
// branch checked out, if any — unlike worktreeForBranch it includes the
// current worktree, since a copy action targets any checkout of the branch.
func (m Model) worktreeAbsPathForBranch(branch string) (string, bool) {
	for _, w := range m.worktrees {
		if w.Branch == branch {
			return w.Path, true
		}
	}
	return "", false
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

// canMoveWorktree gates e / the rename+move menu rows on Worktrees: any
// linked worktree may move — including the current one (the TUI follows the
// move) — but never the main worktree.
func (m Model) canMoveWorktree() bool {
	wt, ok := m.selectedWorktree()
	return m.opsIdle() && ok && len(m.worktrees) > 0 && wt.Path != m.worktrees[0].Path
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
	if !m.opsIdle() {
		return false
	}
	if m.isWipRow(m.commitSelUnified()) {
		return true // a WIP pseudo-row opens its node-vs-parent compare
	}
	_, ok := m.backingIndex(panelCommits)
	return ok
}

// canShowReflogFiles gates l/enter on a reflog row: a resolvable entry under the
// cursor and a wide-enough terminal. Anchors on panelReflog selection only.
func (m Model) canShowReflogFiles() bool {
	if m.focus != panelReflog || !m.opsIdle() {
		return false
	}
	if m.width > 0 && m.width < 40 {
		return false
	}
	_, ok := m.backingIndex(panelReflog)
	return ok
}

// canMark gates m: mark/unmark/pair needs a resolvable row in the focused
// panel (handleMarkKey re-checks and routes the three sub-cases).
func (m Model) canMark() bool {
	if !m.opsIdle() {
		return false
	}
	if m.focus == panelCommits && m.isWipRow(m.commitSelUnified()) {
		return true // a WIP pseudo-row can be marked for compare
	}
	_, ok := m.backingIndex(m.focus)
	return ok
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
	k, ok := m.selectedKey(m.focus) // list-key space; matches a WIP mark too
	return ok && k == m.mark.key
}

// canCommit reports whether there is a staged index to commit and no op is running.
func (m Model) canCommit() bool {
	return m.opsIdle() && m.status.Counts().Staged > 0
}

// canAmend reports whether HEAD has a commit to amend and no op is running.
func (m Model) canAmend() bool {
	return m.opsIdle() && len(m.commits) > 0
}

// canStage reports whether the selected row in a file panel can be staged
// (Files) or unstaged (Staged): a file panel is focused, a row is selected, and
// no op is running.
func (m Model) canStage() bool {
	if !m.isFilesPanel(m.focus) || !m.opsIdle() {
		return false
	}
	_, ok := m.backingIndex(m.focus)
	return ok
}

// canShowFileDiff gates enter on a file panel: the side-by-side diff of the
// selected file (Files: HEAD→working tree; Staged: HEAD→index). Conflicted rows
// are excluded until the conflict editor exists. The width check uses the
// !(w>0 && w<60) idiom so a model that has not seen a WindowSizeMsg yet (tests)
// is not refused.
func (m Model) canShowFileDiff() bool {
	if !m.isFilesPanel(m.focus) {
		return false
	}
	bi, ok := m.backingIndex(m.focus)
	if !ok {
		return false
	}
	f := m.status.Files[bi]
	return m.opsIdle() && f.Kind != model.KindUnmerged && !(m.width > 0 && m.width < 60)
}

// canDiscard gates d/D on the Files panel: at least one discardable
// (non-conflicted) working-tree row exists and no op is running. Conflicted
// files are excluded (they are the x editor's job), matching canShowFileDiff.
func (m Model) canDiscard() bool {
	if m.focus != panelFiles || !m.opsIdle() {
		return false
	}
	for _, f := range m.status.Files {
		if f.Kind != model.KindUnmerged {
			return true
		}
	}
	return false
}

// canDiscardAll gates D on the Files panel: discard the entire working tree. It
// refuses while any conflict exists (conflicts are the x editor's job) and
// requires at least one unstaged or untracked change to throw away. Shared by
// the D dispatch and the footer binding so the footer never advertises D in a
// state where the handler would refuse (e.g. a panel mixing edits + conflicts).
func (m Model) canDiscardAll() bool {
	if m.focus != panelFiles || !m.opsIdle() {
		return false
	}
	if len(m.status.Conflicts()) > 0 {
		return false
	}
	c := m.status.Counts()
	return c.Unstaged > 0 || c.Untracked > 0
}

// canEnterConflict gates x (and its footer hint): the conflict process opens
// when unmerged files exist OR a sequencer op is paused with everything
// resolved (continue/abort still pending — e.g. resolved outside gg). Shared
// by the x dispatch and the footer binding so the footer never advertises a
// state the handler would refuse.
func (m Model) canEnterConflict() bool {
	return m.opsIdle() && (len(m.status.Conflicts()) > 0 || m.conflict.Op != "")
}
