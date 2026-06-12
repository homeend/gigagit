package tui

import (
	"context"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/repos"
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
	headTimes       map[string]int64
	err             error
}

// loadCmd loads status, branches, and recent commits as a single snapshot.
func (m Model) loadCmd() tea.Cmd {
	repo := m.repo
	statePath := m.statePath
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
		// Worktree HEAD commit times power the Worktrees panel's date sort; a
		// failure is non-fatal (dates read as 0 and date sort keeps backing order).
		shas := make([]string, 0, len(out.worktrees))
		for _, w := range out.worktrees {
			if w.Head != "" {
				shas = append(shas, w.Head)
			}
		}
		if times, tErr := repo.CommitTimes(ctx, shas); tErr == nil {
			out.headTimes = times
		}
		// TopLevel marks which listed worktree is the current one; a failure here
		// is non-fatal (the marker just won't show).
		if top, topErr := repo.TopLevel(ctx); topErr == nil {
			// Record this repo in the switcher registry (best-effort; "" = off).
			_ = repos.Touch(statePath, top, time.Now())
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
