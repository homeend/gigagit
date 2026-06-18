package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

func cmdPull(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	background := fs.Bool("background", false, "update the branch's ref without checking it out")
	onConflict := fs.String("on-conflict", "", "how to resolve divergence: rebase|merge|abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *background && *onConflict != "" {
		fmt.Fprintln(stderr, "pull: --on-conflict applies to same-branch divergence, not --background")
		return 2
	}
	intent := engine.PullAndStay
	if *background {
		intent = engine.PullInBackground
	}
	branch := ""
	if fs.NArg() > 0 {
		branch = fs.Arg(0)
	}
	policy := map[string]string{}
	if *onConflict != "" {
		policy["non-fast-forward"] = *onConflict
	}
	dec := cliDecider{policy: policy, in: os.Stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.SmartPull{Branch: branch, Intent: intent}, dec, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdPush(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	cur, err := svc.CurrentBranch(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if cur == "" {
		fmt.Fprintln(stderr, "push: detached HEAD; cannot push")
		return 1
	}
	res, err := runOperation(context.Background(), svc,
		engine.Push{Remote: "origin", Branch: cur, SetUpstream: true}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdSwitch(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(stderr, "switch: a branch name is required")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.SmartSwitch{Branch: args[0]}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdCheckout(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	// Order-independent parse: one optional -s/--switch flag plus the remote ref,
	// so `checkout origin/foo -s` and `checkout -s origin/foo` both work.
	doSwitch := false
	ref := ""
	for _, a := range args {
		switch a {
		case "-s", "--switch":
			doSwitch = true
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(stderr, "checkout: unknown flag %q\n", a)
				return 2
			}
			if ref != "" {
				fmt.Fprintln(stderr, "checkout: too many arguments (expected one <remote>/<branch>)")
				return 2
			}
			ref = a
		}
	}
	if ref == "" {
		fmt.Fprintln(stderr, "checkout: a remote branch (e.g. origin/foo) is required")
		return 2
	}
	remote, local, ok := strings.Cut(ref, "/")
	if !ok || remote == "" || local == "" {
		fmt.Fprintln(stderr, "checkout: expected <remote>/<branch>, e.g. origin/foo")
		return 2
	}
	intent := engine.CheckoutStay
	if doSwitch {
		intent = engine.CheckoutSwitch
	}
	res, err := runOperation(context.Background(), svc,
		engine.SmartCheckout{RemoteRef: ref, Local: local, Intent: intent}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdStash dispatches the stash subcommands (list/apply/pop/drop); with no
// subcommand it pushes a new stash (optionally scoped to paths).
func cmdStash(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "list":
			return cmdStashList(svc, stdout, stderr)
		case "apply":
			return cmdStashRef(svc, args[1:], func(ref string) engine.Operation { return engine.StashApply{Ref: ref} }, stdout, stderr)
		case "pop":
			return cmdStashRef(svc, args[1:], func(ref string) engine.Operation { return engine.StashPop{Ref: ref} }, stdout, stderr)
		case "drop":
			return cmdStashRef(svc, args[1:], func(ref string) engine.Operation { return engine.StashDrop{Ref: ref} }, stdout, stderr)
		}
	}
	fs := flag.NewFlagSet("stash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "gg stash", "stash message")
	untracked := fs.Bool("u", false, "include untracked files")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.Stash{Message: *msg, Paths: fs.Args(), IncludeUntracked: *untracked}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdStashList prints the stash entries, newest first.
func cmdStashList(svc *domain.Service, stdout, stderr io.Writer) int {
	entries, err := svc.StashList(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "%s: %s\n", e.Ref, e.Subject)
	}
	return 0
}

// cmdStashRef runs a stash op against an optional ref (default: the newest
// stash). build maps the ref to the engine op.
func cmdStashRef(svc *domain.Service, args []string, build func(ref string) engine.Operation, stdout, stderr io.Writer) int {
	ref := ""
	if len(args) > 0 {
		ref = args[0]
	}
	res, err := runOperation(context.Background(), svc, build(ref), cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdUndo(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	res, err := runOperation(context.Background(), svc,
		engine.UndoLastCommit{}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// stdinIsTerminal reports whether os.Stdin is an interactive terminal.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
