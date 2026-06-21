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

// validComparePair reports whether (left, right) is one of the four forward
// forms DiffTreeFiles supports: ordered oldest→newest as commit → @staged →
// @worktree, plus commit↔commit. It lets cmdCompare give a friendly message
// instead of leaking the verb's internal "unsupported endpoint pair" error.
func validComparePair(left, right model.Endpoint) bool {
	rank := func(e model.Endpoint) int {
		switch e.Kind {
		case model.EndpointCommit:
			return 0
		case model.EndpointIndex:
			return 1
		default: // worktree
			return 2
		}
	}
	if left.Kind == model.EndpointCommit && right.Kind == model.EndpointCommit {
		return true
	}
	return rank(left) < rank(right)
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
	if !validComparePair(left, right) {
		fmt.Fprintln(stderr, "compare: order endpoints oldest→newest (a commit, then @staged, then @worktree); e.g. `gg compare main @worktree`, not the reverse")
		return 2
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
