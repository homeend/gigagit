package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/gigagit/gg/internal/engine"
)

func cmdPull(repo *repoT, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	background := fs.Bool("background", false, "update the branch's ref without checking it out")
	onConflict := fs.String("on-conflict", "", "how to resolve divergence: rebase|merge|abort")
	if err := fs.Parse(args); err != nil {
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
	res, err := runOperation(context.Background(), repo,
		engine.SmartPull{Branch: branch, Intent: intent}, dec, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdPush(repo *repoT, args []string, stdout, stderr io.Writer) int {
	cur, err := repo.CurrentBranch(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if cur == "" {
		fmt.Fprintln(stderr, "push: detached HEAD; cannot push")
		return 1
	}
	res, err := runOperation(context.Background(), repo,
		engine.Push{Remote: "origin", Branch: cur, SetUpstream: true}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdSwitch(repo *repoT, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(stderr, "switch: a branch name is required")
		return 2
	}
	res, err := runOperation(context.Background(), repo,
		engine.SmartSwitch{Branch: args[0]}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdStash(repo *repoT, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "gg stash", "stash message")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := runOperation(context.Background(), repo,
		engine.Stash{Message: *msg}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

func cmdUndo(repo *repoT, args []string, stdout, stderr io.Writer) int {
	res, err := runOperation(context.Background(), repo,
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
