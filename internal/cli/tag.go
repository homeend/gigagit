package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
)

// cmdTag dispatches the tag subcommands. Stage 1: ls only.
func cmdTag(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0 || args[0] == "ls" || args[0] == "list":
		return cmdTagList(svc, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "tag: unknown subcommand %q (try: ls)\n", args[0])
		return 2
	}
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
