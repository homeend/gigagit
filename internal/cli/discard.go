package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// cmdDiscard implements `gg discard [--yes] (--all | <path>...)`. It throws away
// unstaged changes: tracked edits are restored from the index (staged hunks
// kept), untracked files deleted. Destructive, so it requires --yes — or, on an
// interactive terminal, a y/N confirmation.
func cmdDiscard(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("discard", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "discard ALL unstaged changes")
	yes := fs.Bool("yes", false, "confirm the discard (required; or answer y/N on a TTY)")
	fs.BoolVar(yes, "y", false, "alias for --yes")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	paths := fs.Args()

	if *all && len(paths) > 0 {
		fmt.Fprintln(stderr, "discard: --all takes no paths")
		return 2
	}
	if !*all && len(paths) == 0 {
		fmt.Fprintln(stderr, "usage: gg discard [--yes] (--all | <path>...)")
		return 2
	}

	st, err := svc.Status(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	var op engine.Discard
	var summary string
	if *all {
		if len(st.Conflicts()) > 0 {
			fmt.Fprintln(stderr, "discard: resolve conflicts before discarding all")
			return 2
		}
		op = engine.Discard{All: true}
		summary = "all unstaged changes"
	} else {
		restore, remove, code := classifyDiscard(paths, st, stderr)
		if code != 0 {
			return code
		}
		op = engine.Discard{Restore: restore, Remove: remove}
		summary = fmt.Sprintf("%d file(s)", len(restore)+len(remove))
	}

	if !*yes {
		if !readerIsTerminal(stdin) {
			fmt.Fprintln(stderr, "discard: pass --yes to confirm (this is destructive)")
			return 2
		}
		if !confirmDiscard("Discard "+summary+"? [y/N] ", stdin, stderr) {
			fmt.Fprintln(stderr, "discard: aborted")
			return 0
		}
	}

	res, err := runOperation(context.Background(), svc, op, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// classifyDiscard splits the requested paths into restore (tracked) and remove
// (untracked) lists by looking each up in st. All paths are validated before
// returning: an unmatched or conflicted path fails the whole command (code 2)
// with nothing discarded. code is 0 on success.
func classifyDiscard(paths []string, st model.WorkingTreeStatus, stderr io.Writer) (restore, remove []string, code int) {
	byPath := make(map[string]model.FileStatus, len(st.Files))
	for _, f := range st.Files {
		byPath[f.Path] = f
	}
	for _, p := range paths {
		f, ok := byPath[p]
		if !ok {
			fmt.Fprintf(stderr, "discard: no unstaged change for %q\n", p)
			return nil, nil, 2
		}
		switch f.Kind {
		case model.KindUnmerged:
			fmt.Fprintf(stderr, "discard: %q is conflicted; resolve it first\n", p)
			return nil, nil, 2
		case model.KindUntracked:
			remove = append(remove, p)
		default:
			restore = append(restore, p)
		}
	}
	return restore, remove, 0
}

// readerIsTerminal reports whether r is an interactive terminal. It inspects
// the actual reader passed to the command (os.Stdin in production, a non-file
// reader in tests), so the confirm prompt is offered only when a human can
// answer it — deterministically off for scripted/test stdin.
func readerIsTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// confirmDiscard prints prompt to out and returns true only when the first line
// read from in is an affirmative (y/yes, case-insensitive).
func confirmDiscard(prompt string, in io.Reader, out io.Writer) bool {
	fmt.Fprint(out, prompt)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
