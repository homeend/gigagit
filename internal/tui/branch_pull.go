package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// pullForFocus picks what `p` pulls. On the Branches panel with a NON-current
// branch selected, it pulls that branch in the background (SmartPull moves the
// ref when it's a fast-forward, else stashes → switches → pulls → switches back,
// or pulls in the branch's own worktree) — so the user never leaves their branch.
// Everywhere else (and on the current branch) it pulls the current branch as
// before.
func (m Model) pullForFocus() engine.SmartPull {
	if m.focus == panelBranches {
		if b, ok := m.selectedBranch(); ok && !b.IsHead {
			return engine.SmartPull{Branch: b.Name, Intent: engine.PullInBackground}
		}
	}
	return engine.SmartPull{Intent: engine.PullAndStay}
}

// backgroundPullRow is the Branches-panel `.`-menu action "Pull <branch> (stay
// here)", offered only for a non-current branch (the current branch uses plain
// pull). Runs the same background SmartPull as the context-aware `p` key.
func (m Model) backgroundPullRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok || b.IsHead {
		return actionRow{}, false
	}
	name := b.Name
	return actionRow{
		id:    "pull-branch-bg",
		label: "Pull " + name + " (stay here)",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartPull{Branch: name, Intent: engine.PullInBackground}, i18n.T("Pull %s (stay here)?", name))
		},
	}, true
}
