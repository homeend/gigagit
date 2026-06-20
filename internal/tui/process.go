package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// process owns the interface while active. The single active process
// (Model.proc) IS the interface lock: while it is non-nil all key input routes
// to it, it owns the screen, and every other popup/surface/panel command is
// inert. A process is a set of jobs with its own rules, error handling, and
// resolution; conflict resolution is the first one. Mirrors the surface and
// overlay interfaces, one level up — the gate sits just below the decision modal
// and above everything else, identically in dispatch, render, and mouse.
type process interface {
	// update handles one key while this process owns the interface.
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	// render draws the process's current window; below is the panel interface,
	// so a process can composite over it (a centered list) or replace it (a
	// full-screen editor).
	render(m Model, below string) string
	// finished is called once when a job this process started returns, so the
	// process can advance its state machine and start the next job.
	finished(m Model, res engine.Result, err error) (Model, tea.Cmd)
	// indicator is the status-line text shown while this process is active
	// (what is running and which keys are live).
	indicator(m Model) string
}
