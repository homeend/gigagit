package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
)

// cmdMigrate implements `gg migrate`: it lists every pending store migration
// (a feature preflight found Repairable) and, with no flags, changes
// NOTHING — it only prints what --yes would discard and why. --yes is the
// single consent path for headless use; the TUI and web ask interactively
// instead. There is no confirmation prompt here: a bare invocation must be
// side-effect free, and --yes on the command line already is the consent.
func cmdMigrate(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "apply the migrations (destructive)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	pending, err := svc.PendingMigrations(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "nothing to migrate")
		return 0
	}

	for _, m := range pending {
		fmt.Fprintf(stdout, "%s: format %d -> %d\n", m.Feature, m.From, m.To)
		fmt.Fprintf(stdout, "  %s\n", m.Consequence)
		fmt.Fprintf(stdout, "  discards %d entries\n", len(m.Refs))
	}
	if !*yes {
		fmt.Fprintln(stdout, "\nnothing changed; re-run with --yes to apply")
		return 0
	}
	for _, m := range pending {
		if err := svc.RunMigration(ctx, m); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintf(stdout, "migrated %s to format %d\n", m.Feature, m.To)
	}
	return 0
}
