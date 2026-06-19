package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

// cmdBranch dispatches `gg branch <sub>`.
func cmdBranch(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg branch <create|rename|delete> [args]")
		return 2
	}
	switch args[0] {
	case "create":
		return cmdBranchCreate(svc, args[1:], stdout, stderr)
	case "rename":
		return cmdBranchRename(svc, args[1:], stdout, stderr)
	case "delete":
		return cmdBranchDelete(svc, args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "branch: unknown subcommand %q (use create, rename, or delete)\n", args[0])
		return 2
	}
}

// cmdBranchRename implements `gg branch rename <old> <new>`.
func cmdBranchRename(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] == "" || args[1] == "" {
		fmt.Fprintln(stderr, "usage: gg branch rename <old> <new>")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.RenameBranch{Old: args[0], New: args[1]}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdBranchCreate implements `gg branch create <name> [<start-point>]`.
func cmdBranchCreate(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args) > 2 || args[0] == "" {
		fmt.Fprintln(stderr, "usage: gg branch create <name> [<start-point>]")
		return 2
	}
	start := ""
	if len(args) == 2 {
		start = args[1]
	}
	res, err := runOperation(context.Background(), svc,
		engine.CreateBranch{Name: args[0], StartPoint: start}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdBranchDelete implements `gg branch delete [--force] <name>`. Flags must
// precede the name. The delete-branch confirm is always pre-answered — typing
// the command is the confirmation; --force also pre-answers the unmerged fork.
func cmdBranchDelete(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("branch delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "delete even when not fully merged (git branch -D)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg branch delete [--force] <name>")
		return 2
	}
	policy := map[string]string{"delete-branch": "delete"}
	if *force {
		policy["branch-unmerged"] = "force-delete"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.DeleteBranch{Name: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
