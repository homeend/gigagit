package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// cmdDiff implements `gg diff [--stat|--name-only] [--cached] [<rev>] [-- <paths>]`.
// Default prints the full patch; --stat prints terse "path +A -D" lines with
// an "N files +A -D" trailer; --name-only prints bare paths. Paths must
// follow a "--" separator so a rev is never ambiguous with a path.
func cmdDiff(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stat := fs.Bool("stat", false, "terse per-file change counts")
	nameOnly := fs.Bool("name-only", false, "changed paths only")
	cached := fs.Bool("cached", false, "diff the index against HEAD")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *stat && *nameOnly {
		fmt.Fprintln(stderr, "diff: --stat and --name-only are mutually exclusive")
		return 2
	}
	rev, paths, ok := splitRevAndPaths(fs.Args())
	if !ok {
		fmt.Fprintln(stderr, "usage: gg diff [--stat|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]")
		return 2
	}
	spec := model.DiffSpec{Cached: *cached, Rev: rev, Paths: paths}
	if *stat || *nameOnly {
		stats, err := svc.DiffStat(context.Background(), spec)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if *nameOnly {
			for _, s := range stats {
				fmt.Fprintln(stdout, s.Path)
			}
			return 0
		}
		renderStat(stdout, stats)
		return 0
	}
	patch, err := svc.DiffPatch(context.Background(), spec)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	io.WriteString(stdout, patch)
	return 0
}

// splitRevAndPaths splits positional args at "--": at most one rev before
// it, paths after. Without "--", a single arg is a rev and two or more is
// an error (ambiguous).
func splitRevAndPaths(args []string) (rev string, paths []string, ok bool) {
	for i, a := range args {
		if a == "--" {
			before := args[:i]
			if len(before) > 1 {
				return "", nil, false
			}
			if len(before) == 1 {
				rev = before[0]
			}
			return rev, args[i+1:], true
		}
	}
	switch len(args) {
	case 0:
		return "", nil, true
	case 1:
		return args[0], nil, true
	default:
		return "", nil, false
	}
}

// renderStat prints the terse stat block: "path +A -D" per file ("path bin"
// for binaries, "old => new +A -D" for renames) and an "N files +A -D"
// trailer. Empty input prints nothing.
func renderStat(w io.Writer, stats []model.DiffStat) {
	if len(stats) == 0 {
		return
	}
	add, del := 0, 0
	for _, s := range stats {
		name := s.Path
		if s.OldPath != "" {
			name = s.OldPath + " => " + s.Path
		}
		if s.Binary {
			fmt.Fprintf(w, "%s bin\n", name)
			continue
		}
		add += s.Added
		del += s.Deleted
		fmt.Fprintf(w, "%s +%d -%d\n", name, s.Added, s.Deleted)
	}
	fmt.Fprintf(w, "%d files +%d -%d\n", len(stats), add, del)
}
