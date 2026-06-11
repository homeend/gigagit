package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
)

// Run launches the TUI for repo, taking over the alternate screen until the
// user quits. It returns the directory the shell should switch to (the worktree
// the user switched into during the session, or "" if none) so a wrapper can
// cd there on exit.
func Run(repo *git.Repo) (string, error) {
	p := tea.NewProgram(New(repo), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return "", err
	}
	if m, ok := final.(Model); ok {
		return m.switchTarget, nil
	}
	return "", nil
}
