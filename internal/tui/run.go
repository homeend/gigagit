package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/repos"
)

// Run launches the TUI for svc, taking over the alternate screen until the
// user quits. It returns the directory the shell should switch to (the worktree
// the user switched into during the session, or "" if none) so a wrapper can
// cd there on exit.
func Run(svc *domain.Service) (string, error) {
	m := New(svc)
	m.statePath = repos.DefaultStatePath()
	if home, err := os.UserHomeDir(); err == nil {
		m.initHomeDir = home
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if fm, ok := final.(Model); ok && fm.opCancel != nil {
		fm.opCancel()
	}
	if err != nil {
		return "", err
	}
	if m, ok := final.(Model); ok {
		return m.switchTarget, nil
	}
	return "", nil
}
