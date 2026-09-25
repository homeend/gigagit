package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/steer"
)

// The open-files steer verbs (gg session files, files focus, open
// --background). files and a background open never move the screen, so
// applySteer runs them past steerRefusal; file_focus brings a frame up and
// keeps every refusal.

// steerFiles answers `gg session files`: the current worktree's open files,
// most recently shown first. Read-only — it never touches the screen.
func (m Model) steerFiles(c steer.Command) (Model, tea.Cmd) {
	r := steerOK(c, "")
	r.Files = m.openFilesProto()
	if len(r.Files) == 0 {
		r.Detail = "no open files"
	}
	return m, m.answerSteer(c, r)
}

func (m Model) steerNavigateBackground(c steer.Command) (Model, tea.Cmd) {
	return m, m.answerSteer(c, steerFail(c, "not implemented"))
}

func (m Model) steerFileFocus(c steer.Command) (Model, tea.Cmd) {
	return m, m.answerSteer(c, steerFail(c, "not implemented"))
}
