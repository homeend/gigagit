package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// cmdPrefix implements `gg prefix <ls|add|rm> ...`: the writable two-scope
// registry of branch-name prefixes (skeletons) selectable at create time.
func cmdPrefix(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg prefix <ls|add|rm> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ls", "list":
		return prefixList(svc, rest, stdout, stderr)
	case "add":
		return prefixAdd(svc, rest, stdout, stderr)
	case "rm", "remove":
		return prefixRemove(svc, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "prefix: unknown subcommand %q\n", sub)
		return 2
	}
}

func prefixList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if err := flag.NewFlagSet("prefix ls", flag.ContinueOnError).Parse(args); err != nil {
		return 2
	}
	ps, err := svc.Prefixes(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, p := range ps {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", p.ID, p.Scope.String(), p.Value)
	}
	return 0
}

func prefixAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prefix add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	global := fs.Bool("global", false, "store in the global (every-repo) scope")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg prefix add <value> [--global]")
		return 2
	}
	scope := model.ProfileScopeRepo
	if *global {
		scope = model.ProfileScopeGlobal
	}
	stored, err := svc.AddPrefix(context.Background(), model.Prefix{Value: fs.Arg(0), Scope: scope})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, stored.Value)
	return 0
}

func prefixRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prefix rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	global := fs.Bool("global", false, "remove from the global scope (default: repo)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg prefix rm <value> [--global]")
		return 2
	}
	id := domain.PrefixID(fs.Arg(0))
	scope := model.ProfileScopeRepo
	if *global {
		scope = model.ProfileScopeGlobal
	}
	if err := svc.RemovePrefix(context.Background(), scope, id); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
