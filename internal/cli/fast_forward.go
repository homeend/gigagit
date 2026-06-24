package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdFastForward implements `gg fast-forward <commit>`: advance the current
// branch to <commit> when it is a descendant of HEAD (git merge --ff-only). No
// flags and no decisions — a non-fast-forward target exits non-zero with the
// engine's error on stderr.
func cmdFastForward(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fast-forward", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg fast-forward <commit>")
		return 2
	}
	dec := cliDecider{policy: map[string]string{}, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.FastForward{Commit: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
