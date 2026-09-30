package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/exttool"
)

// The structured-reviews migration: stored copies of the old built-in review
// commands are upgraded to the current templates (exttool.UpgradeReviewCommand)
// in the global config and the active repo config.

// toolConfigPaths are the config files that may hold tool commands:
// the global file and the repo file EffectiveConfig reads.
func (s *Service) toolConfigPaths(ctx context.Context) []string {
	paths := []string{config.DefaultGlobalPath()}
	if p, err := s.activeRepoConfigPath(ctx); err == nil && p != "" {
		paths = append(paths, p)
	}
	return paths
}

// legacyReviewCommandsPresent reports whether a config file holds a review
// command identical to an old built-in. An unreadable file reads as absent:
// the probe fails open, like every legacy probe.
func (s *Service) legacyReviewCommandsPresent(ctx context.Context) bool {
	for _, p := range s.toolConfigPaths(ctx) {
		cmds, err := config.ToolCommandsIn(p)
		if err != nil {
			continue
		}
		for _, tc := range cmds {
			if tc.Category != string(exttool.CatReview) {
				continue
			}
			if _, ok := exttool.UpgradeReviewCommand(tc.Command); ok {
				return true
			}
		}
	}
	return false
}

// upgradeReviewCommands rewrites each recognised old review command in place.
type upgradeReviewCommands struct{ Paths []string }

var _ engine.MigrationAction = upgradeReviewCommands{}

func (u upgradeReviewCommands) Apply(ctx context.Context, deps engine.OpDeps) (int, error) {
	total := 0
	var errs []error
	for _, p := range u.Paths {
		n, err := config.ReplaceToolCommandBodies(p, exttool.UpgradeReviewCommand)
		total += n
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		}
	}
	return total, errors.Join(errs...)
}

func (u upgradeReviewCommands) Describe() string { return "upgrade review commands" }
