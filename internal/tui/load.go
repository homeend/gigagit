package tui

import (
	"context"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/model"
)

// dataLoadedMsg carries a full repo snapshot loaded off the UI thread.
type dataLoadedMsg struct {
	status          model.WorkingTreeStatus
	branches        []model.Branch
	commits         []model.Commit
	worktrees       []model.Worktree
	currentWorktree string
	cfg             config.Config
	gitCommonDir    string
	err             error
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
		if out.worktrees, err = repo.Worktrees(ctx); err != nil {
			out.err = err
			return out
		}
		// TopLevel marks which listed worktree is the current one; a failure here
		// is non-fatal (the marker just won't show).
		if top, topErr := repo.TopLevel(ctx); topErr == nil {
			out.currentWorktree = top
			// Config: built-in defaults overlaid by the global file then the repo's
			// committed .gg.toml. Load errors fall back to defaults.
			if cfg, cfgErr := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml")); cfgErr == nil {
				out.cfg = cfg
			} else {
				out.cfg = config.Defaults()
			}
		} else {
			out.cfg = config.Defaults()
		}
		if gcd, gcdErr := repo.GitCommonDir(ctx); gcdErr == nil {
			out.gitCommonDir = gcd
		}
		return out
	}
}
