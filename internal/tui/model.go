// Package tui implements the gigagit terminal UI with Bubble Tea.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
)

// Model is the root Bubble Tea model.
type Model struct {
	repo          *git.Repo
	width, height int
}

// New constructs the initial model for repo.
func New(repo *git.Repo) Model {
	return Model{repo: repo}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	return "gigagit (loading…)\n"
}

var _ tea.Model = Model{}
