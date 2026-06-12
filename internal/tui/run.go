package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/repos"
)

// Run launches the TUI for repo, taking over the alternate screen until the
// user quits. It returns the directory the shell should switch to (the worktree
// the user switched into during the session, or "" if none) so a wrapper can
// cd there on exit.
func Run(repo *git.Repo) (string, error) {
	m := New(repo)
	m.statePath = repos.DefaultStatePath()
	if home, err := os.UserHomeDir(); err == nil {
		m.initHomeDir = home
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	if m, ok := final.(Model); ok {
		return m.switchTarget, nil
	}
	return "", nil
}
