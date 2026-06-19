package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// canFetchRemotes gates f (fetch) and the Prune . menu action on the Remotes tab.
func (m Model) canFetchRemotes() bool {
	return m.focus == panelRemotes && m.opsIdle()
}

// remotePruneRow offers Prune on the Remotes tab (no dedicated key).
func (m Model) remotePruneRow() (actionRow, bool) {
	if !m.canFetchRemotes() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "prune-remotes",
		label: "Prune remotes (drop deleted branches)",
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.Prune{}) },
	}, true
}
