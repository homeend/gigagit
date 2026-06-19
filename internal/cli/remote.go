package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

// cmdRemote dispatches the remote subcommands: ls | fetch | prune.
func cmdRemote(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "ls" || args[0] == "list":
		return cmdRemoteList(svc, stdout, stderr)
	case args[0] == "fetch":
		res, err := runOperation(context.Background(), svc, engine.Fetch{}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
	case args[0] == "prune":
		res, err := runOperation(context.Background(), svc, engine.Prune{}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "remote: unknown subcommand %q (try: ls, fetch, prune)\n", args[0])
		return 2
	}
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
