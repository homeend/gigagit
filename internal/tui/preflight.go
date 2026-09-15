package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

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
	fmt.Fprintf(out, i18n.T("%s needs a one-time migration")+"\n", m.Feature)
	fmt.Fprintf(out, "  %s\n", renderMigrationConsequence(m))
	fmt.Fprintf(out, "  "+i18n.T("This discards %d entries and cannot be undone.")+"\n", len(m.Refs))
	fmt.Fprintf(out, "  [m] %s  [s] %s  [q] %s: ",
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
