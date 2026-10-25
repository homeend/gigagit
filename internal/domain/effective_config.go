package domain

// The effective gg config for this repo, resolved once in domain so every
// frontend gets the same answer. internal/mcp cannot import internal/cli, and
// applying the [notes] policy is now something both do.

import (
	"context"

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
	committed, private, err := s.RepoConfigPaths(ctx)
	if err != nil {
		return "", err
	}
	return config.ActiveRepoConfigPath(committed, private), nil
}
