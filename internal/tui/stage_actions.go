package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// stageAllRow offers "Stage all files" in the . menu on the file panels
// whenever anything is unstaged: every Files-panel member — untracked files
// and the unstaged half of a partially-staged file included — goes into the
// index in one op, the same engine.Stage space uses. Hidden while a conflict
// exists OR a merge/rebase sits paused (conflicts all resolved, commit still
// pending): mass-staging then would mark conflicts resolved markers-and-all,
// or silently fold unrelated edits into the merge commit (the web files menu
// hides its mass rows for the same reason); the gate mirrors unstageAllRow.
func (m Model) stageAllRow() (actionRow, bool) {
	if !m.isFilesPanel(m.focus) || !m.opsIdle() ||
		len(m.status.Conflicts()) > 0 || m.conflict.Op != "" {
		return actionRow{}, false
	}
	var paths []string
	for _, f := range m.status.Files {
		if inFilesPanel(f) {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "stage-all",
		label: i18n.T("Stage all files"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.running = true
			m.statusMsg = i18n.T("working…")
			return m, m.stageCmd(engine.Stage{Paths: paths})
		},
	}, true
}

// unstageAllRow offers "Unstage all" in the . menu on the file panels whenever
// anything is staged: every Staged-panel member (including the staged half of a
// partially-staged file) is pulled back out of the index in one op — git
// restore --staged, the same engine.Stage{Unstage} space uses. Hidden while a
// conflict exists OR a merge/rebase sits paused (conflicts all resolved,
// commit still pending): unstaging the auto-merged results then would quietly
// hollow out the merge commit (the web files menu hides its mass rows for the
// same reason); the gate mirrors canEnterConflict.
func (m Model) unstageAllRow() (actionRow, bool) {
	if !m.isFilesPanel(m.focus) || !m.opsIdle() ||
		len(m.status.Conflicts()) > 0 || m.conflict.Op != "" {
		return actionRow{}, false
	}
	var paths []string
	for _, f := range m.status.Files {
		if inStagedPanel(f) {
			paths = append(paths, f.Path)
		}
	}
	if len(paths) == 0 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "unstage-all",
		label: i18n.T("Unstage all"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.running = true
			m.statusMsg = i18n.T("working…")
			return m, m.stageCmd(engine.Stage{Paths: paths, Unstage: true})
		},
	}, true
}
