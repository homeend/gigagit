package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
)

// cmdLog implements `gg log [-n N] [<rev>|<A..B>]` — terse history:
// one "<short-sha> <subject>" line per commit, newest first.
func cmdLog(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(stderr)
	n := fs.Int("n", 10, "number of commits to show")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: gg log [-n N] [<rev>|<A..B>]")
		return 2
	}
	rev := "HEAD"
	if fs.NArg() == 1 && fs.Arg(0) != "" {
		rev = fs.Arg(0)
	}
	lines, err := svc.Log(context.Background(), rev, *n)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, l := range lines {
		fmt.Fprintf(stdout, "%s %s\n", l.Hash, l.Subject)
	}
	return 0
}
