package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

func cmdPull(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	background := fs.Bool("background", false, "update the branch's ref without checking it out")
	onConflict := fs.String("on-conflict", "", "how to resolve divergence: rebase|merge|reset|abort (reset = hard-reset to the remote tip, discarding local commits and changes)")
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
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.SmartPull{Branch: branch, Intent: intent}, dec, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdPush implements `gg push [--force | --force-with-lease] [<branch>]`. With no
// positional it pushes the checked-out branch; with a `<branch>` it pushes that
// local branch by name without checking it out (git pushes any local ref). With
// no flag it is a plain push. --force-with-lease force-pushes only if the remote
// has not moved; --force overwrites the remote branch unconditionally (no lease).
// The flags answer the engine's push-force decision, so a force push never
// prompts. (--on-reject=rebase only applies to the current branch — the engine
// refuses to rebase a non-current one, since the rebase would rewrite HEAD.)
func cmdPush(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "force-push, overwriting the remote branch unconditionally (no lease)")
	lease := fs.Bool("force-with-lease", false, "force-push only if the remote branch has not moved")
	onReject := fs.String("on-reject", "", "if a plain push is rejected (remote ahead): rebase|force|force-with-lease|abort (default abort)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *force && *lease {
		fmt.Fprintln(stderr, "push: choose at most one of --force/--force-with-lease")
		return 2
	}
	if *onReject != "" && (*force || *lease) {
		fmt.Fprintln(stderr, "push: --on-reject applies to a plain push, not with --force/--force-with-lease")
		return 2
	}

	target := ""
	switch fs.NArg() {
	case 0:
		// default: the checked-out branch (resolved below)
	case 1:
		target = fs.Arg(0)
	default:
		fmt.Fprintln(stderr, "push: too many arguments (usage: gg push [flags] [<branch>])")
		return 2
	}
	if target == "" {
		cur, err := svc.CurrentBranch(context.Background())
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if cur == "" {
			fmt.Fprintln(stderr, "push: detached HEAD; cannot push (name a branch: gg push <branch>)")
			return 1
		}
		target = cur
	}

	policy := map[string]string{}
	switch {
	case *lease:
		policy["push-force"] = "force-with-lease"
	case *force:
		policy["push-force"] = "force"
	default:
		rp, perr := pushRejectPolicy(*onReject)
		if perr != nil {
			fmt.Fprintln(stderr, perr)
			return 2
		}
		policy = rp
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.Push{Remote: "origin", Branch: target, SetUpstream: true, Force: *force || *lease}, dec, stderr)
	return finish(res, err, stdout, stderr)
}

// pushRejectPolicy maps the --on-reject flag value to the decision policy for a
// rejected plain push. An empty value leaves push-rejected unanswered, so a
// non-interactive push that is rejected fails fast (the decider errors with the
// flag hint) and an interactive one prompts — neither silently no-ops. The
// "force" variants also answer the nested push-force decision so the CLI never
// blocks. An explicit "abort" cancels cleanly (exit 0).
func pushRejectPolicy(onReject string) (map[string]string, error) {
	switch onReject {
	case "":
		return map[string]string{}, nil
	case "abort":
		return map[string]string{"push-rejected": "abort"}, nil
	case "rebase":
		// Also answer the nested rebase-conflict fork: a scripted rebase that
		// conflicts keeps the conflicts (exit non-zero, tree left to resolve),
		// mirroring `gg merge --on-conflict=keep`. Without this the CLI would
		// dead-end on an unanswerable decision.
		return map[string]string{"push-rejected": "rebase", "rebase-conflict": "keep-conflicts"}, nil
	case "force":
		return map[string]string{"push-rejected": "force", "push-force": "force"}, nil
	case "force-with-lease":
		return map[string]string{"push-rejected": "force", "push-force": "force-with-lease"}, nil
	default:
		return nil, fmt.Errorf("push: unknown --on-reject %q (want rebase|force|force-with-lease|abort)", onReject)
	}
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
	// Order-independent parse: -s/--switch, an optional --as <local> (or
	// --as=<local>), and the remote ref, in any order.
	doSwitch := false
	ref, asName := "", ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-s" || a == "--switch":
			doSwitch = true
		case a == "--as":
			i++
			if i >= len(args) {
				fmt.Fprintln(stderr, "checkout: --as requires a branch name")
				return 2
			}
			asName = args[i]
		case strings.HasPrefix(a, "--as="):
			asName = strings.TrimPrefix(a, "--as=")
			if asName == "" {
				fmt.Fprintln(stderr, "checkout: --as requires a branch name")
				return 2
			}
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "checkout: unknown flag %q\n", a)
			return 2
		default:
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
	if asName != "" {
		local = asName
	}
	intent := engine.CheckoutStay
	if doSwitch {
		intent = engine.CheckoutSwitch
	}
	res, err := runOperation(context.Background(), svc,
		engine.SmartCheckout{RemoteRef: ref, Local: local, Intent: intent}, cliDecider{}, stderr)
	code := finish(res, err, stdout, stderr)
	var div engine.CheckoutDivergedError
	if asName == "" && errors.As(err, &div) {
		fmt.Fprintln(stderr, "hint: retry with --as <name> to check it out under a different local name")
	}
	return code
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
