package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdMerge implements `gg merge [--into <target>] [--on-conflict=keep|abort]
// <source>`. Flags precede the positional source branch. --on-conflict
// pre-answers the merge-conflict fork; with neither flag nor TTY the
// conflict surfaces as exit 1 with the options on stderr.
func cmdMerge(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	into := fs.String("into", "", "target branch (default: the current branch)")
	onConflict := fs.String("on-conflict", "", "answer a merge conflict: keep|abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg merge [--into <target>] [--on-conflict=keep|abort] <source>")
		return 2
	}
	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["merge-conflict"] = "keep-conflicts"
	case "abort":
		policy["merge-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "merge: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.SmartMerge{Source: fs.Arg(0), Target: *into}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
