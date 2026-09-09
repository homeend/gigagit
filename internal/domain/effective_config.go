package domain

// The effective gg config for this repo, resolved once in domain so every
// frontend gets the same answer. internal/mcp cannot import internal/cli, and
// applying the [notes] policy is now something both do.

import (
	"context"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
)

// EffectiveConfig loads the effective config (defaults → global → the ACTIVE
// repo file) for this Service's repo: the committed <top>/.gg.toml, overridden
// by a machine-local private file keyed on the MAIN worktree when one exists.
func (s *Service) EffectiveConfig(ctx context.Context) (config.Config, error) {
	top, err := s.TopLevel(ctx)
	if err != nil {
		return config.Config{}, err
	}
	privatePath := ""
	if wts, werr := s.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		privatePath = config.PrivateRepoPath(wts[0].Path)
	}
	active := config.ActiveRepoConfigPath(filepath.Join(top, ".gg.toml"), privatePath)
	return config.Load(config.DefaultGlobalPath(), active)
}
