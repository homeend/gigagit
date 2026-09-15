package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/preflight"
)

// Preflight resolves the repository's feature requirements before the UI
// starts. It returns proceed=false only when a Required feature cannot be
// satisfied, or when the user quits. A pending migration is asked about EVERY
// launch: there is deliberately no suppression, because a feature silently
// left off is the outcome this gate exists to prevent.
//
// Quit is always one of the choices. A pre-UI gate is the one place where
// trapping the user would be unforgivable.
func Preflight(svc *domain.Service, stdin io.Reader, out io.Writer) (bool, error) {
	ctx := context.Background()

	verdicts, err := svc.Preflight(ctx)
	if err != nil {
		return true, nil // a probe failure must not lock the user out
	}
	for _, v := range verdicts {
		if v.Feature.Criticality == preflight.Required && v.State == preflight.Unsatisfiable {
			reason := renderVerdictReason(v)
			return false, fmt.Errorf(i18n.T("gg cannot start: %s"), reason)
		}
	}

	pending, err := svc.PendingMigrations(ctx)
	if err != nil || len(pending) == 0 {
		return true, nil
	}

	r := bufio.NewReader(stdin)
	for _, m := range pending {
		proceed, err := askMigration(m, r, out, func(pm domain.PendingMigration) error {
			return svc.RunMigration(ctx, pm)
		})
		if !proceed || err != nil {
			return proceed, err
		}
	}
	return true, nil
}

// askMigration prompts about one pending migration and reports whether
// launch should proceed. It is split out of Preflight so the actual
// consent/input logic (m/s/q parsing, the read-error-quits and
// skip-leaves-it-alone defaults) is unit-testable against a synthetic
// domain.PendingMigration — today's build declares no feature with a
// Migrate (see domain.Features), so no real repository ever produces a
// pending migration for an end-to-end test to exercise.
func askMigration(m domain.PendingMigration, r *bufio.Reader, out io.Writer, run func(domain.PendingMigration) error) (bool, error) {
	fmt.Fprint(out, migrationBox([]string{
		fmt.Sprintf(i18n.T("%s needs a one-time migration"), m.Feature),
		"",
		renderMigrationConsequence(m),
		"",
		fmt.Sprintf(i18n.T("This discards %d entries and cannot be undone."), len(m.Refs)),
	}))
	fmt.Fprintf(out, "[m] %s  [s] %s  [q] %s: ",
		i18n.T("Migrate"), i18n.T("Skip"), i18n.T("Quit"))

	line, rerr := r.ReadString('\n')
	if rerr != nil && line == "" {
		return false, nil // no input available: treat as quit, never as consent
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "m":
		if err := run(m); err != nil {
			return false, err
		}
	case "q":
		return false, nil
	default: // skip: proceed with the feature disabled
	}
	return true, nil
}

// migrationBoxWidth is the fixed inner text width of the consent box. The
// gate runs before the UI (and before any terminal-size query), so the box
// is sized for the narrowest terminal worth supporting rather than measured.
const migrationBoxWidth = 68

// migrationBox frames paragraphs in a plain box-drawing border, word-wrapping
// each to migrationBoxWidth so every line — heading, consequence prose and
// the irreversibility warning alike — starts at the same column. An empty
// paragraph renders as one blank spacer line.
func migrationBox(paras []string) string {
	var body []string
	for _, p := range paras {
		if strings.TrimSpace(p) == "" {
			body = append(body, "")
			continue
		}
		body = append(body, wrapWords(p, migrationBoxWidth)...)
	}
	var b strings.Builder
	bar := strings.Repeat("\u2500", migrationBoxWidth+2)
	b.WriteString("\u250c" + bar + "\u2510\n")
	for _, line := range body {
		pad := migrationBoxWidth - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		b.WriteString("\u2502 " + line + strings.Repeat(" ", pad) + " \u2502\n")
	}
	b.WriteString("\u2514" + bar + "\u2518\n")
	return b.String()
}
