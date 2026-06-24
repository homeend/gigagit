package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdRemote dispatches the remote subcommands: ls | fetch | prune | rm.
func cmdRemote(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "ls" || args[0] == "list":
		return cmdRemoteList(svc, stdout, stderr)
	case args[0] == "fetch":
		res, err := runOperation(context.Background(), svc, engine.Fetch{}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
	case args[0] == "prune":
		res, err := runOperation(context.Background(), svc, engine.Prune{}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
	case args[0] == "rm" || args[0] == "remove":
		return cmdRemoteRm(svc, args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "remote: unknown subcommand %q (try: ls, fetch, prune, rm)\n", args[0])
		return 2
	}
}

// cmdRemoteRm implements `gg remote rm <remote>/<branch>` — delete a remote
// branch. The command is the confirmation: the delete-remote-branch decision is
// pre-answered. Splits on the FIRST '/' (branch names may contain '/').
func cmdRemoteRm(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: gg remote rm <remote>/<branch>")
		return 2
	}
	remote, branch, ok := strings.Cut(args[0], "/")
	if !ok || remote == "" || branch == "" {
		fmt.Fprintln(stderr, "usage: gg remote rm <remote>/<branch>")
		return 2
	}
	dec := cliDecider{policy: map[string]string{"delete-remote-branch": "delete"}, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.DeleteRemoteBranch{Remote: remote, Branch: branch}, dec, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdRemoteList prints each remote-tracking branch ("origin/foo"), one per line.
func cmdRemoteList(svc *domain.Service, stdout, stderr io.Writer) int {
	rbs, err := svc.RemoteBranches(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, rb := range rbs {
		fmt.Fprintln(stdout, rb.Name)
	}
	return 0
}
