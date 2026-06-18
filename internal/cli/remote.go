package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
)

// cmdRemote dispatches the remote subcommands. Only `ls`/`list` exists today.
func cmdRemote(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "ls" || args[0] == "list" {
		return cmdRemoteList(svc, stdout, stderr)
	}
	fmt.Fprintf(stderr, "remote: unknown subcommand %q (try: ls)\n", args[0])
	return 2
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
