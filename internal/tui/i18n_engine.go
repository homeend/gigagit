package tui

import (
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// This file is the ONE sanctioned place i18n.T is called with a
// non-literal key: engine events carry (format, args) pairs whose format
// literals live in internal/engine and are gate-checked against all four
// bundles by engine_prose_test.go. i18n_scan_test.go allowlists exactly
// this file for the literal-key rule. Do not call i18n.T with a
// non-literal key anywhere else.

// renderSummary renders an operation result's summary in the active
// language. No parts = the English fallback channel.
func renderSummary(res engine.Result) string {
	if len(res.SummaryParts) == 0 {
		return res.Summary
	}
	var b strings.Builder
	for _, p := range res.SummaryParts {
		b.WriteString(i18n.T(p.Format, p.Args...))
	}
	return b.String()
}

// renderProgress renders a progress event as "step" or "step: detail".
// The step is its own catalog key; a DetailMsg-less detail is pure data.
func renderProgress(e engine.Progress) string {
	s := i18n.T(e.Step)
	detail := e.Detail
	if e.DetailMsg.Format != "" {
		detail = i18n.T(e.DetailMsg.Format, e.DetailMsg.Args...)
	}
	if detail != "" {
		s += ": " + detail
	}
	return s
}

// renderPrompt renders a decision prompt in the active language.
func renderPrompt(req engine.DecisionRequest) string {
	if req.PromptMsg.Format != "" {
		return i18n.T(req.PromptMsg.Format, req.PromptMsg.Args...)
	}
	return req.Prompt
}
