package tui

import (
	"os"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// Test seams for guardedReRoot's environment probes. Production always uses
// the real stat and GOOS; tests override both to fabricate the repairable
// verdict, which needs a foreign-notation path that "exists" (the copyTexts
// seam precedent).
var (
	guardStat = func(p string) error { _, err := os.Stat(p); return err }
	guardGOOS = runtime.GOOS
)

// guardedReRoot checks a switch target before any reRoot teardown. Reachable
// → switch as today. Reachable only under the other environment's path
// notation AND offerRepair (the Worktrees-panel enter site) → repair/cancel
// modal; repair runs engine.RepairWorktree on the translated path and, on
// success, chains the switch through pendingRepairSwitch (the
// pendingPushTags capture-only-on-success pattern, wired in opFinishedMsg).
// Anything else → refuse with a status message, session untouched — never
// the raw chdir crash.
func (m Model) guardedReRoot(path string, offerRepair bool) (tea.Model, tea.Cmd) {
	verdict, translated := checkSwitchTarget(guardStat, guardGOOS, path)
	switch verdict {
	case switchOK:
		return m.reRoot(path)
	case switchRepairable:
		if offerRepair {
			return m.offerWorktreeRepair(translated), nil
		}
	}
	m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", path)
	return m, nil
}

// offerWorktreeRepair pushes the frontend-only repair/cancel modal for a
// worktree linked under the other environment's notation. "cancel" is LAST
// so abortOption maps esc to a genuine cancel (never-trap).
func (m Model) offerWorktreeRepair(translated string) Model {
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "worktree-cross-env-repair",
			Prompt:  i18n.T("This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back."),
			Options: []string{"repair", "cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "repair" {
				m.pendingRepairSwitch = translated
				return m.startOp(engine.RepairWorktree{Path: translated})
			}
			return m, nil
		},
	}
	return m
}
