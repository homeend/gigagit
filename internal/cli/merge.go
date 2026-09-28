package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

const mergeUsage = "usage: gg merge [--into <target>] [--on-conflict=keep|abort] [--no-ff] [-m <msg> | -F <file>] <source>"

// cmdMerge implements `gg merge [--into <target>] [--on-conflict=keep|abort]
// [--no-ff] [-m <msg> | -F <file>] <source>`. Flags precede the positional
// source branch. --on-conflict pre-answers the merge-conflict fork; with
// neither flag nor TTY the conflict surfaces as exit 1 with the options on
// stderr. -m / -F (`-` = stdin; the agent lane, where a multi-line message
// with trailers is the norm) set the merge commit's message and imply
// --no-ff — a message is written for a merge COMMIT, and a silent
// fast-forward would drop it.
func cmdMerge(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	into := fs.String("into", "", "target branch (default: the current branch)")
	onConflict := fs.String("on-conflict", "", "answer a merge conflict: keep|abort")
	noFF := fs.Bool("no-ff", false, "always create a merge commit, even when a fast-forward is possible")
	msg := fs.String("m", "", "merge commit message (implies --no-ff)")
	msgFile := fs.String("F", "", "read the merge commit message from a file, or stdin when - (implies --no-ff)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, mergeUsage)
		return 2
	}
	if *msg != "" && *msgFile != "" {
		fmt.Fprintln(stderr, "merge: -m and -F are mutually exclusive")
		return 2
	}
	message := *msg
	if *msgFile != "" {
		var data []byte
		var err error
		if *msgFile == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(*msgFile)
		}
		if err != nil {
			fmt.Fprintf(stderr, "merge: reading message: %v\n", err)
			return 2
		}
		message = string(data)
	}
	message = strings.TrimRight(message, "\r\n\t ")
	if (*msg != "" || *msgFile != "") && message == "" {
		fmt.Fprintln(stderr, "merge: empty message")
		return 2
	}
	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["merge-conflict"] = "keep-conflicts"
	case "abort":
		policy["merge-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "merge: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	// Resolved the same way the engine resolves an empty Target (current
	// branch) so printDrift checks the branch this merge actually moved,
	// even when --into was not given.
	target := *into
	if target == "" {
		target, _ = svc.CurrentBranch(context.Background())
	}
	res, err := runOperation(context.Background(), svc,
		engine.SmartMerge{Source: fs.Arg(0), Target: *into, Message: message, NoFF: *noFF}, dec, stderr)
	code := finish(res, err, stdout, stderr)
	// res.Changed, not just a zero exit: finish returns 0 whenever err == nil,
	// and --on-conflict=abort returns Result{Changed:false}, nil. The pre-op
	// snapshot was already written and the tip never moved, so an abort would
	// report every path upstream added since the fork as deleted — an alarm
	// after an operation that changed nothing. The TUI and web gate the same way.
	if code == 0 && res.Changed {
		printDrift(context.Background(), svc, target, stdout)
	}
	return code
}
