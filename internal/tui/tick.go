package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// timerMsg is a timer in quiet mode: the headless loop parks it and fires
// it only when a step asks (Headless.FireTimers) — time never passes in a
// golden-screen test.
type timerMsg struct {
	due  time.Duration
	fire func(time.Time) tea.Msg
}

// tick is the TUI's one timer: tea.Tick normally, a timerMsg descriptor in
// quiet mode (headless.go).
func (m Model) tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	if m.quiet {
		return func() tea.Msg { return timerMsg{due: d, fire: fn} }
	}
	return tea.Tick(d, fn)
}
