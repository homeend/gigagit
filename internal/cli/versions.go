package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

const versionsUsage = "usage: gg versions [<branch>] | gg versions restore [--discard] <branch> <id|latest>"

// cmdVersions implements `gg versions [<branch>]` (list) and dispatches
// `gg versions restore …` to cmdVersionsRestore. List prints one line per
// recorded pre-operation snapshot, newest first: "<id> <short-sha> <ISO-time>
// <subject>"; an empty result prints "(no versions)" (still exit 0 — an
// unrecorded branch is not an error). Branch defaults to the current branch;
// a detached HEAD with no branch argument is a usage-level failure (exit 1),
// since there is nothing to default to.
func cmdVersions(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "restore" {
		return cmdVersionsRestore(svc, args[1:], stdin, stdout, stderr)
	}
	fs := flag.NewFlagSet("versions", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, versionsUsage)
		return 2
	}
	ctx := context.Background()
	branch := ""
	if fs.NArg() == 1 {
		branch = fs.Arg(0)
	} else {
		cur, err := svc.CurrentBranch(ctx)
		if err != nil || cur == "" {
			fmt.Fprintln(stderr, "error: no current branch (detached HEAD) — name a branch")
			return 1
		}
		branch = cur
	}
	rows, err := svc.BranchVersions(ctx, branch)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "(no versions)")
		return 0
	}
	for _, v := range rows {
		short := v.Hash
		if len(short) > 8 {
			short = short[:8]
		}
		id := fmt.Sprintf("%d-%s", v.Unix, v.Op)
		when := time.Unix(v.Unix, 0).Format("2006-01-02T15:04")
		fmt.Fprintf(stdout, "%s %s %s %s\n", id, short, when, v.Subject)
	}
	return 0
}

// cmdVersionsRestore implements `gg versions restore [--discard] <branch>
// <id|latest>`: resolves id (an "<unix>-<op>" token from `gg versions`, or
// the literal "latest" for the newest row) to its version ref via
// svc.BranchVersions, then runs engine.RestoreBranchVersion. --discard
// pre-answers the "restore-dirty" decision (the current-branch lane's
// uncommitted-changes fork) with "proceed"; without it, a dirty tree fails
// loud under the CLI's non-interactive default.
func cmdVersionsRestore(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("versions restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	discard := fs.Bool("discard", false, "discard uncommitted changes if the restore needs a hard reset")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, versionsUsage)
		return 2
	}
	branch, id := fs.Arg(0), fs.Arg(1)
	ctx := context.Background()
	rows, err := svc.BranchVersions(ctx, branch)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	var ref string
	if id == "latest" {
		if len(rows) > 0 {
			ref = rows[0].Ref
		}
	} else {
		for _, v := range rows {
			if fmt.Sprintf("%d-%s", v.Unix, v.Op) == id {
				ref = v.Ref
				break
			}
		}
	}
	if ref == "" {
		fmt.Fprintf(stderr, "error: no version %q of branch %s (try `gg versions %s`)\n", id, branch, branch)
		return 1
	}
	policy := map[string]string{}
	if *discard {
		policy["restore-dirty"] = "proceed"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(ctx, svc, engine.RestoreBranchVersion{Branch: branch, Ref: ref}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
