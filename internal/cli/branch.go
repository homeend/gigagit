package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdBranch dispatches `gg branch <sub>`.
func cmdBranch(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg branch <create|rename|delete|current|ls> [args]")
		return 2
	}
	switch args[0] {
	case "create":
		return cmdBranchCreate(svc, args[1:], stdout, stderr)
	case "rename":
		return cmdBranchRename(svc, args[1:], stdout, stderr)
	case "delete":
		return cmdBranchDelete(svc, args[1:], stdin, stdout, stderr)
	case "current":
		return cmdBranchCurrent(svc, stdout, stderr)
	case "ls":
		return cmdBranchLs(svc, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "branch: unknown subcommand %q (use create, rename, delete, current, or ls)\n", args[0])
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

// cmdBranchCurrent implements `gg branch current` — the bare branch name,
// or HEAD's short sha when detached.
func cmdBranchCurrent(svc *domain.Service, stdout, stderr io.Writer) int {
	name, err := svc.CurrentBranch(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if name == "" { // detached HEAD
		line, lerr := svc.CommitLine(context.Background(), "HEAD")
		if lerr != nil {
			fmt.Fprintln(stderr, "error:", lerr)
			return 1
		}
		name = line.Hash
	}
	fmt.Fprintln(stdout, name)
	return 0
}

// cmdBranchLs implements `gg branch ls` — one local branch per line,
// "* " marking HEAD, "↑a ↓b" only when an upstream exists.
func cmdBranchLs(svc *domain.Service, stdout, stderr io.Writer) int {
	branches, err := svc.Branches(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, b := range branches {
		marker := "  "
		if b.IsHead {
			marker = "* "
		}
		if b.Upstream != "" {
			fmt.Fprintf(stdout, "%s%s ↑%d ↓%d\n", marker, b.Name, b.Ahead, b.Behind)
		} else {
			fmt.Fprintf(stdout, "%s%s\n", marker, b.Name)
		}
	}
	return 0
}
