package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// commitEditRow offers a single-commit history edit (move/drop) on the
// Commits panel. Gated to a non-merge, non-root commit while a branch is
// checked out. The actual rebase base + plan are computed asynchronously after
// the commit range loads (startCommitEditCmd → rebaseRangeLoadedMsg).
func (m Model) commitEditRow(id, label string, e rebaseplan.Edit) (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() || m.status.Branch == "" {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok || len(m.commits[bi].Parents) != 1 { // non-merge, non-root
		return actionRow{}, false
	}
	sha := m.commits[bi].Hash
	branch := m.status.Branch
	return actionRow{
		id:    id,
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startCommitEditCmd(branch, sha, e)
		},
	}, true
}

func (m Model) commitDropRow() (actionRow, bool) {
	return m.commitEditRow("commit-drop", "Drop commit", rebaseplan.EditDrop)
}

func (m Model) commitMoveUpRow() (actionRow, bool) {
	return m.commitEditRow("commit-move-up", "Move commit up (newer)", rebaseplan.EditMoveUp)
}

func (m Model) commitMoveDownRow() (actionRow, bool) {
	return m.commitEditRow("commit-move-down", "Move commit down (older)", rebaseplan.EditMoveDown)
}

// startCommitEditCmd derives the rebase base (rebaseplan.OntoFor) and loads the
// commit range to edit.
func (m Model) startCommitEditCmd(branch, sha string, e rebaseplan.Edit) (Model, tea.Cmd) {
	return m, m.loadRebaseRangeCmd(branch, rebaseplan.OntoFor(sha, e), sha, e)
}
