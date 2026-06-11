package cli

import (
	"context"
	"fmt"
	"io"
)

// cmdWorktree dispatches `gg worktree <sub>`.
func cmdWorktree(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg worktree <list|add> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdWorktreeList(repo, stdout, stderr)
	case "add":
		return cmdWorktreeAdd(repo, args[1:], stdin, stdout, stderr, cwdFile)
	default:
		fmt.Fprintf(stderr, "worktree: unknown subcommand %q (use list or add)\n", args[0])
		return 2
	}
}

func cmdWorktreeList(repo *repoT, stdout, stderr io.Writer) int {
	wts, err := repo.Worktrees(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, w := range wts {
		branch := w.Branch
		if branch == "" {
			branch = "(detached)"
		}
		fmt.Fprintf(stdout, "%s\t%s\n", branch, w.Path)
	}
	return 0
}

// cmdWorktreeAdd is implemented in Task 5.
func cmdWorktreeAdd(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	fmt.Fprintln(stderr, "worktree add: not yet implemented")
	return 2
}
