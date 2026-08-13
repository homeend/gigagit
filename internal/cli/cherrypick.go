package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdCherryPick implements `gg cherry-pick [--on-conflict=keep|abort] <commit>`.
// It applies <commit> onto the current branch as a new commit. --on-conflict
// pre-answers the cherry-pick-conflict fork; with neither flag nor TTY the
// conflict surfaces as exit 1 with the options on stderr.
func cmdCherryPick(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cherry-pick", flag.ContinueOnError)
	fs.SetOutput(stderr)
	onConflict := fs.String("on-conflict", "", "answer a cherry-pick conflict: keep|abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg cherry-pick [--on-conflict=keep|abort] <commit>")
		return 2
	}
	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["cherry-pick-conflict"] = "keep-conflicts"
	case "abort":
		policy["cherry-pick-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "cherry-pick: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.CherryPick{Commits: []string{fs.Arg(0)}}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
