package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// pushBranchRow is the Branches-panel `.`-menu action "Push <branch>", offered
// for the selected branch — current or not. The `P` key only ever pushes the
// checked-out branch (m.status.Branch); this row lets the user push whichever
// branch they have highlighted, which git does by name without a checkout
// (`git push -u origin <branch>`). SetUpstream records tracking on a branch that
// was never pushed before. Push touches no working tree, so it runs directly
// (no slow-op confirm, like the `P` key and the force-push row).
func (m Model) pushBranchRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	name := b.Name
	return actionRow{
		id:    "push-branch",
		label: i18n.T("Push %s", name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.Push{Remote: "origin", Branch: name, SetUpstream: true})
		},
	}, true
}
