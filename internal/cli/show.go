package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
)

// cmdShow implements `gg show <commit> [--patch] [-- <file>...]` — a
// "<short-sha> <subject>" header followed by the terse stat block
// (default) or the full patch (--patch).
func cmdShow(svc *domain.Service, dir string, args []string, stdout, stderr io.Writer) int {
	head, paths := splitDashDash(args) // BEFORE fs.Parse — flag.Parse eats a leading "--"
	paths = repoPathspecs(svc, dir, paths)
	// flag.Parse stops at the first non-flag argument, but the usage string
	// (and docs) teach `gg show <commit> --patch` — flag AFTER the positional.
	// Partition head into flag-ish args ("-" prefixed) and positionals before
	// parsing; this is only safe because show's one flag, --patch, is a bool
	// (a bool flag never consumes a following arg as its value).
	flagArgs, posArgs := partitionFlags(head)
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print the full patch instead of the stat block")
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(posArgs) != 1 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "usage: gg show <commit> [--patch] [-- <file>...]")
		return 2
	}
	rev := posArgs[0]
	if isLinkArg(rev) {
		if len(paths) > 0 {
			fmt.Fprintln(stderr, "show: a gg:// link already names the file; drop the -- <paths>")
			return 2
		}
		// show keeps the refusing default: it anchors on ONE commit, and a
		// pair's only single commit (B) is not that commit — it is the
		// change-set's newer end (ruling R4).
		res, err := resolveLinkArgShapes(context.Background(), svc, rev, linkShapes{Ref: true}, "show")
		if err != nil {
			return linkExit("show", err, stderr)
		}
		if res.Preview != nil {
			fmt.Fprintln(stderr, "show: that link names a merge preview, not a commit; use gg diff <link>")
			return 2
		}
		if res.Addr.Commit == "" {
			fmt.Fprintln(stderr, "show: that link names the working tree; gg show needs a link to a commit (gg://<repo>/<path>@<sha>)")
			return 2
		}
		svc = openLinkTarget(res)
		rev = res.Addr.Commit
		if res.Addr.Path != "" {
			paths = []string{res.Addr.Path}
		}
	}
	ctx := context.Background()
	if *patch {
		line, text, err := svc.ShowPatch(ctx, rev, paths)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s %s\n", line.Hash, line.Subject)
		io.WriteString(stdout, text)
		return 0
	}
	line, stats, err := svc.ShowStat(ctx, rev, paths)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s %s\n", line.Hash, line.Subject)
	renderStat(stdout, stats)
	return 0
}
