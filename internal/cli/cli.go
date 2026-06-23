package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/repos"
)

// RepoStatePath is the repo-switcher registry location. "" disables recording
// and yields an empty registry — cmd/gg wires the real path; tests stay
// hermetic by default.
var RepoStatePath string

// InitHomeDir is the home directory used for `gg init`'s home-scoped agent
// detection. "" skips home-scoped agents — cmd/gg wires the real home; tests
// stay hermetic by default.
var InitHomeDir string

// Run dispatches a CLI subcommand against the repo at workdir, writing to
// stdout/stderr, and returns a process exit code.
func Run(workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	// stderr is shared between the main goroutine (progress, prompts, errors)
	// and the operation goroutine (decider prompts) — serialize it once here.
	stderr = &syncWriter{w: stderr}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg <command> [args]")
		return 2
	}
	svc := domain.Open(workdir)
	cmd, rest := args[0], args[1:]
	// Record this repo in the switcher registry (best-effort: errors and
	// non-repo working directories are ignored). Skip for "repo" subcommands
	// since they are registry management commands, not git operations, and may
	// be run from arbitrary directories.
	if RepoStatePath != "" && cmd != "repo" {
		if top, err := svc.TopLevel(context.Background()); err == nil {
			_ = repos.Touch(RepoStatePath, top, time.Now())
		}
	}
	switch cmd {
	case "status":
		return cmdStatus(svc, stdout, stderr)
	case "commit":
		return cmdCommit(svc, rest, stdout, stderr)
	case "pull":
		return cmdPull(svc, rest, stdout, stderr)
	case "push":
		return cmdPush(svc, rest, stdin, stdout, stderr)
	case "switch":
		return cmdSwitch(svc, rest, stdout, stderr)
	case "checkout":
		return cmdCheckout(svc, rest, stdout, stderr)
	case "branch":
		return cmdBranch(svc, rest, stdin, stdout, stderr)
	case "stash":
		return cmdStash(svc, rest, stdout, stderr)
	case "undo":
		return cmdUndo(svc, rest, stdout, stderr)
	case "discard":
		return cmdDiscard(svc, rest, stdin, stdout, stderr)
	case "shelf":
		return cmdShelf(svc, rest, stdin, stdout, stderr)
	case "bookmark":
		return cmdBookmark(svc, rest, stdin, stdout, stderr)
	case "merge":
		return cmdMerge(svc, rest, stdin, stdout, stderr)
	case "rebase":
		return cmdRebase(svc, rest, stdin, stdout, stderr)
	case "cherry-pick":
		return cmdCherryPick(svc, rest, stdin, stdout, stderr)
	case "revert":
		return cmdRevert(svc, rest, stdin, stdout, stderr)
	case "reset":
		return cmdReset(svc, rest, stdin, stdout, stderr)
	case "fast-forward":
		return cmdFastForward(svc, rest, stdin, stdout, stderr)
	case "worktree":
		return cmdWorktree(svc, rest, stdin, stdout, stderr, cwdFile)
	case "remote":
		return cmdRemote(svc, rest, stdin, stdout, stderr)
	case "tag":
		return cmdTag(svc, rest, stdout, stderr)
	case "compare":
		return cmdCompare(svc, rest, stdout, stderr)
	case "repo":
		return cmdRepo(rest, stdout, stderr, cwdFile)
	case "init":
		return cmdInit(workdir, rest, stdin, stdout, stderr)
	case "config":
		return cmdConfig(svc, workdir, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		return 2
	}
}

var commands = map[string]bool{
	"status": true, "commit": true, "pull": true, "push": true,
	"switch": true, "checkout": true, "branch": true, "stash": true, "undo": true, "merge": true, "rebase": true, "worktree": true,
	"cherry-pick": true, "revert": true, "reset": true, "fast-forward": true,
	"discard": true, "shelf": true, "bookmark": true,
	"remote": true, "tag": true, "compare": true,
	"inspect": true, "repo": true, "init": true, "config": true,
}

// IsCommand reports whether tok is a gg CLI subcommand (used by cmd/gg to
// choose between the CLI and launching the TUI). Note: "inspect" is routed by
// cmd/gg to its own handler, not by Run.
func IsCommand(tok string) bool { return commands[tok] }

func cmdStatus(svc *domain.Service, stdout, stderr io.Writer) int {
	st, err := svc.Status(context.Background())
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

func cmdCommit(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "reword" {
		return cmdCommitReword(svc, args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	msg := fs.String("m", "", "commit message (required unless --amend)")
	all := fs.Bool("all", false, "stage modified/deleted tracked files first (-a)")
	fs.BoolVar(all, "a", false, "alias for --all")
	amend := fs.Bool("amend", false, "rewrite the last commit instead of creating one")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	message := *msg
	if message == "" {
		if !*amend {
			fmt.Fprintln(stderr, "commit: -m message is required")
			return 2
		}
		// --amend with no -m: reuse the existing message.
		prev, err := svc.LastCommitMessage(context.Background())
		if err != nil {
			fmt.Fprintln(stderr, "commit: cannot read last message:", err)
			return 1
		}
		message = strings.TrimRight(prev, "\n")
	}
	res, err := runOperation(context.Background(), svc,
		engine.Commit{Message: message, All: *all, Amend: *amend}, cliDecider{}, stderr)
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
