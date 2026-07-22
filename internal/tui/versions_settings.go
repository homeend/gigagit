package tui

import (
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// versionsPolicyFromConfig maps the [versions] config section to the engine
// policy injected into every op via svc.SetVersionsPolicy. Disabled is
// inverted (see config.VersionsConfig's doc comment); MaxAgeDays passes
// through as-is (0 = default 90 is resolved by config.Load's overlay, -1 =
// keep forever, handled by the engine's pruneBranchVersions).
func versionsPolicyFromConfig(cfg config.Config) engine.VersionsPolicy {
	return engine.VersionsPolicy{Enabled: !cfg.Versions.Disabled, MaxAgeDays: cfg.Versions.MaxAgeDays}
}

// saveVersionsRetention persists `[versions] max_age_days` (-1 = keep
// forever) to the active repo config and updates the live policy so the next
// operation's branch-version snapshot honors it immediately, without waiting
// for a reload. A day count of 0 or below -1 is rejected (0 is meaningless —
// -1 already means "never prune" — and anything below -1 has no defined
// meaning) with a status message instead of being written.
func (m Model) saveVersionsRetention(days int) Model {
	if days == 0 || days < -1 {
		m.statusMsg = i18n.T("retention must be a positive day count or -1 (keep forever)")
		return m
	}
	m.cfg.Versions.MaxAgeDays = days
	m.svc.SetVersionsPolicy(versionsPolicyFromConfig(m.cfg))
	if m.repoConfigPath == "" {
		m.statusMsg = i18n.T("retention set (not saved: no repo config path)")
		return m
	}
	if err := config.SetVersionsMaxAgeDays(m.repoConfigPath, days); err != nil {
		m.statusMsg = i18n.T("retention set but not saved: %s", err.Error())
	}
	return m
}

// toggleVersionsRecording flips whether branch-version snapshots are
// recorded at all, persisting the choice to the active repo config and
// updating the live policy (mirrors saveVersionsRetention).
func (m Model) toggleVersionsRecording() Model {
	m.cfg.Versions.Disabled = !m.cfg.Versions.Disabled
	m.svc.SetVersionsPolicy(versionsPolicyFromConfig(m.cfg))
	if m.repoConfigPath == "" {
		m.statusMsg = i18n.T("recording toggled (not saved: no repo config path)")
		return m
	}
	if err := config.SetVersionsDisabled(m.repoConfigPath, m.cfg.Versions.Disabled); err != nil {
		m.statusMsg = i18n.T("recording toggled but not saved: %s", err.Error())
	}
	return m
}
