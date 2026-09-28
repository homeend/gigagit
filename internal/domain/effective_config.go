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
	active, err := s.activeRepoConfigPath(ctx)
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(config.DefaultGlobalPath(), active)
}

// activeRepoConfigPath is the repo config file EffectiveConfig overlays.
func (s *Service) activeRepoConfigPath(ctx context.Context) (string, error) {
	top, err := s.TopLevel(ctx)
	if err != nil {
		return "", err
	}
	privatePath := ""
	if wts, werr := s.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		privatePath = config.PrivateRepoPath(wts[0].Path)
	}
	return config.ActiveRepoConfigPath(filepath.Join(top, ".gg.toml"), privatePath), nil
}
