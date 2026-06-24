package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/repos"
)

// cmdRepo dispatches `gg repo <list|switch>` — the repo-switcher registry.
// Switching is frontend state (print + cwd-file), not a git mutation, so no
// engine operation is involved.
func cmdRepo(args []string, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg repo <list|switch> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdRepoList(stdout)
	case "switch":
		return cmdRepoSwitch(args[1:], stdout, stderr, cwdFile)
	default:
		fmt.Fprintf(stderr, "repo: unknown subcommand %q (use list or switch)\n", args[0])
		return 2
	}
}

func cmdRepoList(stdout io.Writer) int {
	for _, e := range repos.Load(RepoStatePath) {
		fmt.Fprintf(stdout, "%s\t%s\n", repos.Name(e), e.Path)
	}
	return 0
}

func cmdRepoSwitch(args []string, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(stderr, "repo switch: a query is required")
		return 2
	}
	q := strings.ToLower(args[0])
	var matches []repos.Entry
	for _, e := range repos.Load(RepoStatePath) {
		if strings.Contains(strings.ToLower(repos.Name(e)), q) ||
			strings.Contains(strings.ToLower(e.Path), q) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		fmt.Fprintf(stderr, "repo switch: no known repository matches %q\n", args[0])
		return 1
	case 1:
		fmt.Fprintln(stdout, matches[0].Path)
		if cwdFile != "" {
			_ = os.WriteFile(cwdFile, []byte(matches[0].Path), 0o644)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "repo switch: %q is ambiguous:\n", args[0])
		for _, e := range matches {
			fmt.Fprintf(stderr, "  %s\t%s\n", repos.Name(e), e.Path)
		}
		return 1
	}
}
