package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdReset implements `gg reset [--soft|--mixed|--hard] [--force] <commit>`. It
// moves the current branch to <commit>. The mode flags answer the reset-mode
// decision (no flag → mixed, git's default). When <commit> is not on the current
// branch the op asks for confirmation; --force pre-answers it, otherwise a
// non-ancestor reset on a non-TTY exits 1 with the options on stderr.
func cmdReset(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	fs.SetOutput(stderr)
	soft := fs.Bool("soft", false, "keep the changes staged")
	mixed := fs.Bool("mixed", false, "keep the changes unstaged (default)")
	hard := fs.Bool("hard", false, "discard uncommitted tracked changes")
	force := fs.Bool("force", false, "proceed even if the target is not on the current branch")
	fs.BoolVar(force, "f", false, "alias for --force")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg reset [--soft|--mixed|--hard] [--force] <commit>")
		return 2
	}

	n := 0
	mode := "mixed" // git's default when no flag is given
	if *soft {
		n, mode = n+1, "soft"
	}
	if *mixed {
		n, mode = n+1, "mixed"
	}
	if *hard {
		n, mode = n+1, "hard"
	}
	if n > 1 {
		fmt.Fprintln(stderr, "reset: choose at most one of --soft/--mixed/--hard")
		return 2
	}

	policy := map[string]string{"reset-mode": mode}
	if *force {
		policy["reset-confirm"] = "proceed"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.Reset{Commit: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
