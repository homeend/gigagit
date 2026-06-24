package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/app"
	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/cli"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/shellinit"
	"github.com/homeend/gigagit/internal/tui"
)

func main() {
	cli.RepoStatePath = repos.DefaultStatePath()
	if home, err := os.UserHomeDir(); err == nil {
		cli.InitHomeDir = home
	}
	cwdFile, args := extractCwdFile(os.Args[1:])
	timeTrack, args := extractTimeTrack(args)
	if timeTrack != "" {
		if err := setupTimeTrack(timeTrack, args); err != nil {
			fmt.Fprintln(os.Stderr, "gg: --time-track:", err)
			os.Exit(2)
		}
	}
	if len(args) > 0 && args[0] == "shell-init" {
		runShellInit(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "inspect" {
		runInspect(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "__rebase-seq" {
		if err := runRebaseSeq(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "gg __rebase-seq:", err)
			os.Exit(1)
		}
		return
	}
	if len(args) > 0 && args[0] == "__rebase-message" {
		if err := runRebaseMessage(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "gg __rebase-message:", err)
			os.Exit(1)
		}
		return
	}
	if len(args) > 0 && cli.IsCommand(args[0]) {
		os.Exit(cli.Run(".", args, os.Stdin, os.Stdout, os.Stderr, cwdFile))
	}
	// A mistyped/unknown subcommand should error, not silently open the TUI.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(os.Stderr, "gg: unknown command %q\n", args[0])
		fmt.Fprintln(os.Stderr, "commands: status commit pull push switch checkout branch stash undo discard shelf bookmark merge rebase cherry-pick revert reset worktree remote tag compare repo init inspect (run `gg` with no arguments for the TUI)")
		os.Exit(2)
	}
	// No subcommand: launch the TUI.
	ring := observ.NewRing(200)
	// Wrap with LimitRunner so the initial session shares the process-global
	// subprocess bound — matching domain.OpenTUI, which the reRoot path uses.
	// WithSSHBatchMode so an ssh prompt fails fast instead of freezing the TUI.
	repo := &git.Repo{Runner: gitexec.NewLimitRunner(gitexec.NewExecRunner("git", ".", ring).WithSSHBatchMode())}
	defer func() {
		if r := recover(); r != nil {
			path := filepath.Join(os.TempDir(), fmt.Sprintf("gg-panic-%d.json", time.Now().Unix()))
			_ = app.DumpRepo(context.Background(), path, repo, ring, []string{fmt.Sprintf("panic: %v", r)})
			fmt.Fprintf(os.Stderr, "gg panicked; debug dump written to %s\n", path)
			panic(r)
		}
	}()
	svc := domain.New(repo)
	// Pre-flight: surface the common "not a git repository" / missing-git case as
	// a friendly message instead of launching the TUI only for it to fail with a
	// raw "git status failed (exit 128): fatal: …" dump.
	if _, err := svc.TopLevel(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, friendlyGitError(err))
		os.Exit(1)
	}
	cwd, err := tui.Run(svc)
	if err != nil {
		fmt.Fprintln(os.Stderr, friendlyGitError(err))
		os.Exit(1)
	}
	// Only write the cwd file when the user actually switched worktrees, so a
	// gg-wrapped shell stays put otherwise.
	if cwdFile != "" && cwd != "" {
		_ = os.WriteFile(cwdFile, []byte(cwd), 0o644)
	}
}

// extractCwdFile pulls a global --cwd-file flag (in either "--cwd-file path" or
// "--cwd-file=path" form) out of args, returning its value and the remaining
// args. A trailing "--cwd-file" with no value is dropped.
func extractCwdFile(args []string) (string, []string) {
	path := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--cwd-file":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--cwd-file="):
			path = strings.TrimPrefix(a, "--cwd-file=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest
}

// extractTimeTrack pulls the global --time-track flag (in either
// "--time-track path" or "--time-track=path" form) out of args, returning its
// value and the remaining args. A trailing "--time-track" with no value is
// dropped.
func extractTimeTrack(args []string) (string, []string) {
	path := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--time-track":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--time-track="):
			path = strings.TrimPrefix(a, "--time-track=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest
}

// setupTimeTrack opens path for appending (creating it if missing), routes
// every span there, and emits the run-delimiting "gg start" span carrying the
// (redacted) argv and the build version. The file is never explicitly closed:
// each span is one unbuffered line, and process exit releases the handle.
func setupTimeTrack(path string, argv []string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	observ.SetSpanSink(f)
	observ.EmitSpan(observ.Span{
		Name:  "gg start",
		Args:  append(append([]string{}, argv...), "version="+buildinfo.Version),
		Start: time.Now(),
	})
	return nil
}

func runShellInit(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: gg shell-init [bash|zsh|fish]")
		os.Exit(2)
	}
	script, err := shellinit.Script(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Fprint(os.Stdout, script)
}

func runInspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	dumpPath := fs.String("debug-dump", "", "write a debug dump JSON file to this path")
	trace := fs.Bool("trace", false, "enable verbose timing trace to stderr")
	_ = fs.Parse(args)

	opts := app.Options{WorkDir: ".", Stdout: os.Stdout, DumpPath: *dumpPath}
	if *trace || os.Getenv("GG_TRACE") == "1" {
		opts.Trace = os.Stderr
	}
	if err := app.Inspect(context.Background(), opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
