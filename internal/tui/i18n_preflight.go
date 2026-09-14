package tui

import (
	"errors"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/preflight"
)

// This file is the second sanctioned place i18n.T is called with a
// non-literal key, alongside i18n_engine.go: a preflight.Verdict's Reason
// carries a (format, args) pair whose format literals live in
// internal/preflight (a stdlib-only DAG leaf that stays English at the
// source) and are gate-checked against all four bundles by
// preflightProseKeys in engine_prose_test.go — mirroring engineProseKeys for
// internal/engine. i18n_scan_test.go allowlists exactly this file (and
// i18n_engine.go) for the literal-key rule.
func renderVerdictReason(v preflight.Verdict) string {
	return i18n.T(v.Reason.Format, v.Reason.Args...)
}

// renderVerdictRemedy localizes an Unsatisfiable verdict's remedy — what
// fixes the feature from OUTSIDE gg (upgrade git, upgrade gg). Same catalog
// rules as renderVerdictReason: the literal lives in internal/preflight and
// is gate-checked by preflightProseKeys.
func renderVerdictRemedy(v preflight.Verdict) string {
	return i18n.T(v.Remedy.Format, v.Remedy.Args...)
}

// renderMigrationConsequence localizes a pending migration's consent prose.
// The consent screen is the surface the spec's Prose section names by name:
// it describes IRREVERSIBLE data loss, so it must read in the user's own
// language. domain carries the (format, args) pair unrendered precisely so
// this call can exist; the same catalog-key rules as renderVerdictReason
// apply, and the gate in engine_prose_test.go checks every Describe literal
// (in internal/preflight AND internal/domain) against all four bundles.
func renderMigrationConsequence(m domain.PendingMigration) string {
	if m.ConsequenceFormat == "" {
		return m.Consequence
	}
	return i18n.T(m.ConsequenceFormat, m.ConsequenceArgs...)
}

// renderFeatureDisabled localizes a domain.ErrFeatureDisabled. Its Error()
// string is the agent-facing English protocol value (the CLI prints it
// verbatim by design); a TUI surface must never show that raw inside a
// localized UI, so the wrapper sentence is a literal catalog key here and
// the reason goes through the same path as a Verdict's.
func renderFeatureDisabled(e *domain.ErrFeatureDisabled) string {
	reason := e.Reason
	if e.ReasonFormat != "" {
		reason = i18n.T(e.ReasonFormat, e.ReasonArgs...)
	}
	return i18n.T("the %s feature is unavailable in this repository: %s", e.ID, reason)
}

// renderLoadError is the popup-body wrapper for a failed background load. A
// domain.ErrFeatureDisabled is a DECIDED, user-facing outcome rather than a
// git failure, so it renders as localized prose; anything else keeps the
// existing "(load failed: %s)" shape with git's own English text inside.
func renderLoadError(err error) string {
	var disabled *domain.ErrFeatureDisabled
	if errors.As(err, &disabled) {
		return renderFeatureDisabled(disabled)
	}
	return i18n.T("(load failed: %s)", err.Error())
}
