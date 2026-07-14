package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// branchMergeRow offers "Merge <branch> into current" on the Branches tab.
// SmartMerge with an empty Target defaults to the current branch; conflicts and
// dirty trees are handled by SmartMerge's Decider ladder (mapped to the TUI
// modal). Mirrors remoteMergeRow/tagMergeRow, plus a !IsHead guard: a local
// selection can be the current branch, and merging a branch into itself is
// degenerate (the engine rejects Source==Target). Hidden on detached HEAD.
func (m Model) branchMergeRow() (actionRow, bool) {
	b, ok := m.selectedBranch()
	if m.focus != panelBranches || !m.opsIdle() || !ok || b.IsHead {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	return actionRow{
		id:    "branch-merge",
		label: "Merge " + b.Name + " into current (" + cur + ")",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartMerge{Source: b.Name}, i18n.T("Merge %s into current branch?", b.Name))
		},
	}, true
}

// branchRebaseRow offers "Rebase current onto <branch>" on the Branches tab.
// SmartRebase with an empty Branch defaults to the current branch. Same gating as
// branchMergeRow (incl. the !IsHead guard). Hidden on detached HEAD.
func (m Model) branchRebaseRow() (actionRow, bool) {
	b, ok := m.selectedBranch()
	if m.focus != panelBranches || !m.opsIdle() || !ok || b.IsHead {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	return actionRow{
		id:    "branch-rebase",
		label: "Rebase current (" + cur + ") onto " + b.Name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartRebase{Onto: b.Name}, i18n.T("Rebase current branch onto %s?", b.Name))
		},
	}, true
}
