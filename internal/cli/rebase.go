package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// cmdRebase implements `gg rebase [--branch <b>] [--on-conflict=keep|abort]
// <newbase>`. Flags precede the positional newbase (the new base, = Onto).
// --branch selects the branch to rebase (default: the current branch).
// --on-conflict pre-answers the rebase-conflict fork; with neither flag nor
// TTY the conflict surfaces as exit 1 with the options on stderr.
func cmdRebase(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rebase", flag.ContinueOnError)
	fs.SetOutput(stderr)
	branch := fs.String("branch", "", "branch to rebase (default: the current branch)")
	onConflict := fs.String("on-conflict", "", "answer a rebase conflict: keep|abort")
	interactive := fs.Bool("i", false, "interactive rebase, driven by --plan")
	fs.BoolVar(interactive, "interactive", false, "alias for -i")
	planPath := fs.String("plan", "", "interactive rebase plan file (JSON); requires -i")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg rebase [-i --plan <file>] [--branch <b>] [--on-conflict=keep|abort] <newbase>")
		return 2
	}
	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["rebase-conflict"] = "keep-conflicts"
	case "abort":
		policy["rebase-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "rebase: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}

	if *interactive || *planPath != "" {
		if !*interactive || *planPath == "" {
			fmt.Fprintln(stderr, "rebase: -i requires --plan <file> (the TUI builds the plan interactively)")
			return 2
		}
		raw, err := os.ReadFile(*planPath)
		if err != nil {
			fmt.Fprintln(stderr, "rebase: --plan:", err)
			return 2
		}
		plan, err := rebaseplan.Unmarshal(raw)
		if err != nil {
			fmt.Fprintln(stderr, "rebase: --plan: invalid plan:", err)
			return 2
		}
		br := *branch
		if br == "" {
			br, err = svc.CurrentBranch(context.Background())
			if err != nil {
				fmt.Fprintln(stderr, "rebase:", err)
				return 1
			}
		}
		ggBin, err := os.Executable()
		if err != nil {
			fmt.Fprintln(stderr, "rebase:", err)
			return 1
		}
		res, err := runOperation(context.Background(), svc,
			engine.InteractiveRebase{Branch: br, Onto: fs.Arg(0), Plan: plan, GGBin: ggBin}, dec, stderr)
		return finish(res, err, stdout, stderr)
	}

	res, err := runOperation(context.Background(), svc,
		engine.SmartRebase{Branch: *branch, Onto: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
