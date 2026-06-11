package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// dataLoadedMsg carries a full repo snapshot loaded off the UI thread.
type dataLoadedMsg struct {
	status   model.WorkingTreeStatus
	branches []model.Branch
	commits  []model.Commit
	err      error
}

// loadCmd loads status, branches, and recent commits as a single snapshot.
func (m Model) loadCmd() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		ctx := context.Background()
		var out dataLoadedMsg
		st, err := repo.Status(ctx)
		if err != nil {
			out.err = err
			return out
		}
		out.status = st
		if out.branches, err = repo.Branches(ctx); err != nil {
			out.err = err
			return out
		}
		if out.commits, err = repo.Log(ctx, 50); err != nil {
			out.err = err
			return out
		}
		return out
	}
}
