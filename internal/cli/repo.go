package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

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
	writeRepoTable(stdout, "", repos.Load(RepoStatePath))
	return 0
}

func cmdRepoSwitch(args []string, stdout, stderr io.Writer, cwdFile string) int {
	exact := false
	var rest []string
	for _, a := range args {
		if a == "--exact" {
			exact = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) != 1 || rest[0] == "" {
		fmt.Fprintln(stderr, "usage: gg repo switch [--exact] <query>")
		return 2
	}
	query := rest[0]
	var matches []repos.Entry
	for _, e := range repos.Load(RepoStatePath) {
		if repoMatches(e, query, exact) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		fmt.Fprintf(stderr, "repo switch: no known repository matches %q\n", query)
		return 1
	case 1:
		fmt.Fprintln(stdout, matches[0].Path)
		if cwdFile != "" {
			_ = os.WriteFile(cwdFile, []byte(matches[0].Path), 0o644)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "repo switch: %q is ambiguous:\n", query)
		writeRepoTable(stderr, "  ", matches)
		return 1
	}
}

// repoMatches reports whether registry entry e answers query: a
// case-insensitive substring of its name or path, or — under exact — the
// whole name or the whole (cleaned) path, so a query that prefixes sibling
// checkouts still names exactly one of them.
func repoMatches(e repos.Entry, query string, exact bool) bool {
	name, path := repos.Name(e), e.Path
	if exact {
		return strings.EqualFold(name, query) ||
			strings.EqualFold(filepath.Clean(path), filepath.Clean(query))
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(name), q) ||
		strings.Contains(strings.ToLower(path), q)
}

// writeRepoTable prints one "<name>  <path>" row per entry, space-padding the
// names so every path starts in the same column.
func writeRepoTable(w io.Writer, indent string, entries []repos.Entry) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, e := range entries {
		fmt.Fprintf(tw, "%s%s\t%s\n", indent, repos.Name(e), e.Path)
	}
	_ = tw.Flush()
}
