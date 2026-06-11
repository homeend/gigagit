// Package tui implements the gigagit terminal UI with Bubble Tea.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/model"
)

// Model is the root Bubble Tea model.
type Model struct {
	repo          *git.Repo
	width, height int

	loading  bool
	err      error
	status   model.WorkingTreeStatus
	branches []model.Branch
	commits  []model.Commit
}

// New constructs the initial model for repo.
func New(repo *git.Repo) Model {
	return Model{repo: repo, loading: true}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return m.loadCmd() }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case dataLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.status = msg.status
			m.branches = msg.branches
			m.commits = msg.commits
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.loading = true
			return m, m.loadCmd()
		}
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.loading {
		return "gigagit (loading…)\n"
	}
	if m.err != nil {
		return "error: " + m.err.Error() + "\n"
	}
	return "gigagit — branch " + m.status.Branch + "\n"
}

var _ tea.Model = Model{}
