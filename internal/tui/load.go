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

// dataLoadedMsg carries a full repo snapshot loaded off the UI thread. gen is
// the load generation it was issued for; a result whose gen no longer matches
// the model's loadGen is stale (superseded by a newer load) and dropped.
type dataLoadedMsg struct {
	gen             int
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

// loadCmd loads the repo snapshot via the domain layer (gated, parallel,
// coalesced) and, on success, layers in the non-git config + MRU touch. It
// bakes in the current loadGen so a stale result can be dropped.
func (m Model) loadCmd() tea.Cmd {
	svc := m.svc
	statePath := m.statePath
	gen := m.loadGen
	return func() tea.Msg {
		ctx := context.Background()
		snap, err := svc.Snapshot(ctx)
		if err != nil {
			return dataLoadedMsg{gen: gen, err: err}
		}
		out := dataLoadedMsg{
			gen:             gen,
			status:          snap.Status,
			branches:        snap.Branches,
			commits:         snap.Commits,
			worktrees:       snap.Worktrees,
			currentWorktree: snap.CurrentWorktree,
			gitCommonDir:    snap.GitCommonDir,
			headTimes:       snap.HeadTimes,
			cfg:             config.Defaults(),
		}
		// config and the MRU registry are not git reads; do them here, after
		// the gated snapshot, keyed off the toplevel it reported.
		if snap.CurrentWorktree != "" {
			_ = repos.Touch(statePath, snap.CurrentWorktree, time.Now())
			if cfg, cfgErr := config.Load(config.DefaultGlobalPath(), filepath.Join(snap.CurrentWorktree, ".gg.toml")); cfgErr == nil {
				out.cfg = cfg
			}
		}
		return out
	}
}
