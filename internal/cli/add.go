package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdAdd implements `gg add [-f] (-A | <path>...)` — stage named paths, or
// with -A everything including untracked files. -f answers the engine's
// stage.ignored decision with force-add, so gitignored paths stage without a
// prompt; without it a refusal over ignored paths prompts on a terminal and
// fails non-interactively (matching git's own behavior).
func cmdAdd(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("A", false, "stage all changes, including untracked files")
	force := fs.Bool("f", false, "stage paths even when .gitignore excludes them (git add -f)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *all == (fs.NArg() > 0) { // exactly one of -A / paths
		fmt.Fprintln(stderr, "usage: gg add [-f] (-A | <path>...)")
		return 2
	}
	policy := map[string]string{}
	if *force {
		policy[engine.IgnoredPathsDecisionID] = "force-add"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.Stage{Paths: fs.Args(), All: *all}, dec, stderr)
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
