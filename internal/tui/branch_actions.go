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
		label: i18n.T("Merge %s into current (%s)", b.Name, cur),
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
		label: i18n.T("Rebase current (%s) onto %s", cur, b.Name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartRebase{Onto: b.Name}, i18n.T("Rebase current branch onto %s?", b.Name))
		},
	}, true
}

// branchVersionsRow offers "Previous versions…" on the Branches tab, opening
// the versionsPopup straight into versions mode for the selected branch. It
// IS gated on versionsFeatureEnabled, which now costs a git read every time
// the . menu opens (svc.Preflight re-validates its cache with one
// for-each-ref) — that's accepted as cheap. It is deliberately NOT
// self-gated on "does this branch have any recorded versions": the popup
// itself shows "no versions recorded" when the list is empty.
func (m Model) branchVersionsRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() || !m.versionsFeatureEnabled() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelBranches)
	if !ok || bi < 0 || bi >= len(m.branches) {
		return actionRow{}, false
	}
	name := m.branches[bi].Name
	return actionRow{
		id:    "branch-versions",
		label: i18n.T("Previous versions…"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openBranchVersions(name, false, false)
		},
	}, true
}

// showInWorktreesRow offers "Show in Worktrees" on a branch that is checked
// out in some worktree — the current one included, as the copy-path row. It
// is navigation, not an op (no opsIdle gate, no re-root: "Switch to branch"
// already offers to GO to that worktree).
func (m Model) showInWorktreesRow() (actionRow, bool) {
	b, ok := m.selectedBranch()
	if m.focus != panelBranches || !ok {
		return actionRow{}, false
	}
	if _, has := m.worktreeAbsPathForBranch(b.Name); !has {
		return actionRow{}, false
	}
	name := b.Name
	return actionRow{
		id:    "show-in-worktrees",
		label: i18n.T("Show in Worktrees"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.showBranchWorktree(name), nil },
	}, true
}

// showBranchWorktree switches to the Worktrees tab with the cursor on the
// worktree that has branch checked out. A `/` filter on that list is cleared
// so the row can be selected; a filter bound to another panel is left alone.
// The worktree is looked up at RUN time — the list may have refreshed since
// the menu was built — and a vanished one is a notice, not a tab switch.
func (m Model) showBranchWorktree(branch string) Model {
	wi := -1
	for i, w := range m.worktrees {
		if w.Branch == branch {
			wi = i
			break
		}
	}
	if wi < 0 {
		m.statusMsg = i18n.T("%s is no longer checked out in a worktree", branch)
		return m
	}
	if m.filterPanel == panelWorktrees {
		m.filterTyping = false
		m.filterQuery = ""
	}
	m = m.activateTab(panelWorktrees)
	ents := m.worktreeEntries()
	for di, u := range m.displayIndices(panelWorktrees) {
		if u < len(ents) && ents[u].wt == wi && !ents[u].sub() {
			m.sel[panelWorktrees] = di
			break
		}
	}
	return m
}

// browseRemoteBranchesRow offers the browse-remote-branches picker from the
// Remotes panel menu. Panel-scoped, not row-scoped: no selection required.
func (m Model) browseRemoteBranchesRow() (actionRow, bool) {
	if m.focus != panelRemotes || !m.opsIdle() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "browse-remote-branches",
		label: i18n.T("Browse remote branches…"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.openRemoteHeadsBrowser() },
	}, true
}
