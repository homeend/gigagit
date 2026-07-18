package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// forcePushRow offers "Force push <branch>" on the Branches panel when the
// selected branch is the one checked out in this worktree (push targets the
// current branch). It launches engine.Push with Force set; the engine then asks
// the push-force decision (force-with-lease / force / abort), so the modal both
// chooses the lease-vs-plain variant and confirms the history-overwriting push.
// No dedicated key — discoverable via the `.` menu, like Rename branch.
func (m Model) forcePushRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() || m.status.Branch == "" {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok || !b.IsHead {
		return actionRow{}, false
	}
	return actionRow{
		id:    "force-push",
		label: i18n.T("Force push %s", b.Name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.Push{Remote: "origin", Branch: m.status.Branch, SetUpstream: true, Force: true})
		},
	}, true
}
