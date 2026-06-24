package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdRevert implements `gg revert [--on-conflict=keep|abort] <commit>`. It
// creates a new commit on the current branch undoing <commit>. --on-conflict
// pre-answers the revert-conflict fork; with neither flag nor TTY the conflict
// surfaces as exit 1 with the options on stderr. Reverting a merge commit is
// refused by git (it needs -m <parent>) and surfaces as a legible error.
func cmdRevert(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("revert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	onConflict := fs.String("on-conflict", "", "answer a revert conflict: keep|abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg revert [--on-conflict=keep|abort] <commit>")
		return 2
	}
	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["revert-conflict"] = "keep-conflicts"
	case "abort":
		policy["revert-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "revert: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.Revert{Commit: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
