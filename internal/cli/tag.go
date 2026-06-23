package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

// cmdTag dispatches the tag subcommands: ls | create | rm | checkout.
func cmdTag(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "ls" || args[0] == "list":
		return cmdTagList(svc, stdout, stderr)
	case args[0] == "create":
		return cmdTagCreate(svc, args[1:], stdout, stderr)
	case args[0] == "rm" || args[0] == "delete":
		return cmdTagDelete(svc, args[1:], stdin, stdout, stderr)
	case args[0] == "checkout" || args[0] == "co":
		return cmdTagCheckout(svc, args[1:], stdout, stderr)
	case args[0] == "push":
		return cmdTagPush(svc, args[1:], stdout, stderr)
	case args[0] == "annotate":
		return cmdTagAnnotate(svc, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "tag: unknown subcommand %q (try: ls, create, rm, checkout, push, annotate)\n", args[0])
		return 2
	}
}

// cmdTagPush implements `gg tag push <name> [<remote>]`. With no remote and a
// single configured remote it pushes there; with multiple it errors (specify one).
func cmdTagPush(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args) > 2 || args[0] == "" {
		fmt.Fprintln(stderr, "usage: gg tag push <name> [<remote>]")
		return 2
	}
	remote := ""
	if len(args) == 2 {
		remote = args[1]
	}
	res, err := runOperation(context.Background(), svc,
		engine.PushTag{Name: args[0], Remote: remote}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdTagCheckout implements `gg tag checkout [--branch <name>] <tag>`. With
// --branch it creates that branch at the tag and switches; otherwise detached.
func cmdTagCheckout(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag checkout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	branch := fs.String("branch", "", "create this branch at the tag and switch to it")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) != 1 || rest[0] == "" {
		fmt.Fprintln(stderr, "usage: gg tag checkout [--branch <name>] <tag>")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.Checkout{Ref: rest[0], Branch: *branch}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdTagDelete implements `gg tag rm [--remote] <name> [<remote>]` (alias
// delete). Default deletes the tag locally (typing the command is the
// confirmation). --remote deletes it on the remote instead: the remote is the
// optional second positional, else auto-resolved; the confirm is pre-answered.
func cmdTagDelete(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	remote := fs.Bool("remote", false, "delete the tag on the remote (git push --delete) instead of locally")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg tag rm [--remote] <name> [<remote>]")
		return 2
	}
	name := fs.Arg(0)

	if !*remote {
		res, err := runOperation(context.Background(), svc,
			engine.DeleteTag{Name: name}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
	}

	rem := ""
	if fs.NArg() >= 2 {
		rem = fs.Arg(1)
	}
	dec := cliDecider{policy: map[string]string{"delete-remote-tag": "delete"}, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.DeleteRemoteTag{Tag: name, Remote: rem}, dec, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdTagCreate implements `gg tag create [-m <msg>] <name> [<commit>]` (flags
// precede the name, like the other gg subcommands). An empty message makes a
// lightweight tag; -m makes it annotated. Commit defaults to HEAD.
func cmdTagCreate(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "annotated tag message (empty = lightweight)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) < 1 || len(rest) > 2 || rest[0] == "" {
		fmt.Fprintln(stderr, "usage: gg tag create [-m <msg>] <name> [<commit>]")
		return 2
	}
	commit := ""
	if len(rest) == 2 {
		commit = rest[1]
	}
	res, err := runOperation(context.Background(), svc,
		engine.CreateTag{Name: rest[0], Commit: commit, Message: *msg}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdTagAnnotate implements `gg tag annotate -m <msg> <name>` — force-recreate
// the tag as annotated at its current target. Flags precede the name.
func cmdTagAnnotate(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag annotate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "annotation message (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg tag annotate -m <message> <name>")
		return 2
	}
	if *msg == "" {
		fmt.Fprintln(stderr, "tag annotate: -m <message> is required")
		return 2
	}
	name := fs.Arg(0)
	target, err := svc.RevParse(context.Background(), name+"^{commit}")
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	res, err := runOperation(context.Background(), svc,
		engine.CreateTag{Name: name, Commit: target, Message: *msg, Force: true}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdTagList prints each tag name, one per line (newest first).
func cmdTagList(svc *domain.Service, stdout, stderr io.Writer) int {
	tags, err := svc.Tags(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, t := range tags {
		fmt.Fprintln(stdout, t.Name)
	}
	return 0
}
