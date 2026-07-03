package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// cmdDiff implements `gg diff [--stat|--name-only] [--cached] [<rev>] [-- <paths>]`.
// Default prints the full patch; --stat prints terse "path +A -D" lines with
// an "N files +A -D" trailer; --name-only prints bare paths. Paths must
// follow a "--" separator so a rev is never ambiguous with a path.
func cmdDiff(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	head, paths := splitDashDash(args)
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stat := fs.Bool("stat", false, "terse per-file change counts")
	nameOnly := fs.Bool("name-only", false, "changed paths only")
	cached := fs.Bool("cached", false, "diff the index against HEAD")
	if err := fs.Parse(head); err != nil {
		return 2
	}
	if *stat && *nameOnly {
		fmt.Fprintln(stderr, "diff: --stat and --name-only are mutually exclusive")
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: gg diff [--stat|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]")
		return 2
	}
	rev := ""
	if fs.NArg() == 1 {
		rev = fs.Arg(0)
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

// splitDashDash splits raw args at the first literal "--": everything
// before it goes to flag parsing (flags plus at most one rev), everything
// after is paths. The split must happen BEFORE fs.Parse — flag.Parse
// consumes a leading "--" itself, which would misread the first path as
// a rev when no rev is given.
func splitDashDash(args []string) (head, paths []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// partitionFlags splits args (already past splitDashDash, so no literal "--"
// remains) into flag-ish args ("-" prefixed) and positionals, preserving
// relative order within each group. This lets a command accept its flag
// either before or after its positional (e.g. `gg show HEAD --patch`), which
// flag.Parse alone can't do — it stops at the first non-flag argument. It is
// only safe for command flag sets made entirely of bool flags: a bool flag
// never consumes a following argument as its value, so moving flag-ish
// tokens out of position can't strand an intended flag value as a positional.
func partitionFlags(args []string) (flagArgs, posArgs []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
		} else {
			posArgs = append(posArgs, a)
		}
	}
	return flagArgs, posArgs
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
