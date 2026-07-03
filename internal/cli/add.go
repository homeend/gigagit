package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdAdd implements `gg add (-A | <path>...)` — stage named paths, or with
// -A everything including untracked files.
func cmdAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("A", false, "stage all changes, including untracked files")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *all == (fs.NArg() > 0) { // exactly one of -A / paths
		fmt.Fprintln(stderr, "usage: gg add (-A | <path>...)")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.Stage{Paths: fs.Args(), All: *all}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdUnstage implements `gg unstage <path>...` — remove paths from the
// index, keeping working-tree content.
func cmdUnstage(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg unstage <path>...")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.Stage{Paths: args, Unstage: true}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
