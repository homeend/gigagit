package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

// parseEndpoint maps a CLI token to a comparison endpoint: "@worktree" (the
// working tree), "@staged"/"@index" (the index), or any other token as a
// commit-ish (git resolves HEAD, branch names, abc123, HEAD~2, …).
func parseEndpoint(s string) model.Endpoint {
	switch s {
	case "@worktree":
		return model.Endpoint{Kind: model.EndpointWorkTree}
	case "@staged", "@index":
		return model.Endpoint{Kind: model.EndpointIndex}
	default:
		return model.Endpoint{Kind: model.EndpointCommit, Hash: s}
	}
}

// cmdCompare prints the changed-file list between two endpoints:
//
//	gg compare <left> [<right>]
//
// where each endpoint is a commit-ish, @staged, or @worktree. <right> defaults
// to @worktree. Output is one "<status>\t<path>" line per changed file.
func cmdCompare(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg compare <left> [<right>]   (endpoints: a commit, @staged, @worktree; right defaults to @worktree)")
		return 2
	}
	left := parseEndpoint(args[0])
	right := model.Endpoint{Kind: model.EndpointWorkTree}
	if len(args) > 1 {
		right = parseEndpoint(args[1])
	}
	files, err := svc.CompareFiles(context.Background(), left, right)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, f := range files {
		if f.OldPath != "" {
			fmt.Fprintf(stdout, "%s\t%s -> %s\n", f.Status, f.OldPath, f.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\n", f.Status, f.Path)
	}
	return 0
}
