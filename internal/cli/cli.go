package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
)

type repoT = git.Repo

// Run dispatches a CLI subcommand against the repo at workdir, writing to
// stdout/stderr, and returns a process exit code.
func Run(workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg <command> [args]")
		return 2
	}
	repo := openRepo(workdir)
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "status":
		return cmdStatus(repo, stdout, stderr)
	case "commit":
		return cmdCommit(repo, rest, stdout, stderr)
	case "pull":
		return cmdPull(repo, rest, stdout, stderr)
	case "push":
		return cmdPush(repo, rest, stdout, stderr)
	case "switch":
		return cmdSwitch(repo, rest, stdout, stderr)
	case "stash":
		return cmdStash(repo, rest, stdout, stderr)
	case "undo":
		return cmdUndo(repo, rest, stdout, stderr)
	case "worktree":
		return cmdWorktree(repo, rest, stdin, stdout, stderr, cwdFile)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		return 2
	}
}

var commands = map[string]bool{
	"status": true, "commit": true, "pull": true, "push": true,
	"switch": true, "stash": true, "undo": true, "worktree": true, "inspect": true,
}

// IsCommand reports whether tok is a gg CLI subcommand (used by cmd/gg to
// choose between the CLI and launching the TUI). Note: "inspect" is routed by
// cmd/gg to its own handler, not by Run.
func IsCommand(tok string) bool { return commands[tok] }

func cmdStatus(repo *repoT, stdout, stderr io.Writer) int {
	st, err := repo.Status(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	line := "on branch " + st.Branch
	if st.Upstream != "" {
		line += fmt.Sprintf(" (%s ↑%d ↓%d)", st.Upstream, st.Ahead, st.Behind)
	}
	fmt.Fprintln(stdout, line)
	if c := st.Counts(); c.Staged+c.Unstaged+c.Untracked+c.Conflicted == 0 {
		fmt.Fprintln(stdout, "working tree clean")
		return 0
	}
	for _, f := range st.Files {
		x, y := f.Staged, f.Unstaged
		if x == 0 {
			x = ' '
		}
		if y == 0 {
			y = ' '
		}
		fmt.Fprintf(stdout, "%c%c %s\n", x, y, f.Path)
	}
	return 0
}

func cmdCommit(repo *repoT, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "commit message (required)")
	all := fs.Bool("all", false, "stage modified/deleted tracked files first (-a)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *msg == "" {
		fmt.Fprintln(stderr, "commit: -m message is required")
		return 2
	}
	res, err := runOperation(context.Background(), repo,
		engine.Commit{Message: *msg, All: *all}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// finish prints the result summary (or error) and maps to an exit code.
func finish(res engine.Result, err error, stdout, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if res.Summary != "" {
		fmt.Fprintln(stdout, "✓ "+res.Summary)
	}
	return 0
}
