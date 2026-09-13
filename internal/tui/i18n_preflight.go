package tui

import (
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
