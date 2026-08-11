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
	"github.com/homeend/gigagit/internal/mcp"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/shellinit"
	"github.com/homeend/gigagit/internal/tui"
	"github.com/homeend/gigagit/internal/web"
)

func main() {
	cli.RepoStatePath = repos.DefaultStatePath()
	if home, err := os.UserHomeDir(); err == nil {
		cli.InitHomeDir = home
	}
	cwdFile, args := extractCwdFile(os.Args[1:])
	timeTrack, args := extractTimeTrack(args)
	recordPath, args := extractRecord(args)
	if timeTrack != "" {
		if err := setupTimeTrack(timeTrack, args); err != nil {
			fmt.Fprintln(os.Stderr, "gg: --time-track:", err)
			os.Exit(2)
		}
	}
	if len(args) > 0 && isVersionRequest(args[0]) {
		fmt.Fprintln(os.Stdout, buildinfo.String())
		return
	}
	if len(args) > 0 && args[0] == "shell-init" {
		runShellInit(args[1:])
		return
	}
	// Everything past here touches the repo; a shell sitting in a deleted
	// directory would only get git's raw getcwd failure, so explain it here.
	if msg := staleCwdMessage(); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(1)
	}
	if len(args) > 0 && args[0] == "inspect" {
		runInspect(args[1:])
		return
	}
	if len(args) > 0 && args[0] == "mcp" {
		if err := mcp.Serve(context.Background(), "."); err != nil {
			fmt.Fprintln(os.Stderr, "gg mcp:", err)
			os.Exit(1)
		}
		return
	}
	if len(args) > 0 && args[0] == "web" {
		fs := flag.NewFlagSet("web", flag.ExitOnError)
		addr := fs.String("addr", "", "listen address (loopback only; default 127.0.0.1:0)")
		open := fs.Bool("open", false, "open the system browser at the served URL")
		_ = fs.Parse(args[1:])
		// The always-on error log the TUI keeps (errors.log beside
		// operations.log): the web server's genuine failures were previously
		// ring-only — /api/session-errors shows the ring either way, but the
		// durable file should not depend on which frontend ran.
		if ef, _, eerr := tui.OpenErrorLog(); eerr == nil && ef != nil {
			observ.SetFailureSink(ef)
			defer func() { observ.SetFailureSink(nil); _ = ef.Close() }()
		}
		if err := web.Serve(context.Background(), ".", *addr, *open); err != nil {
			fmt.Fprintln(os.Stderr, "gg web:", err)
			os.Exit(1)
		}
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
		// Kept in sync with cli.commands (plus the commands main routes itself:
		// shell-init, inspect, mcp, web, version).
		fmt.Fprintln(os.Stderr, "commands: status commit pull push switch checkout branch stash undo merge rebase fast-forward cherry-pick revert reset discard add unstage log diff show compare shelf bookmark prefix worktree remote tag versions review apply unlock config repo init batch mcp web shell-init inspect version (run `gg` with no arguments for the TUI)")
		os.Exit(2)
	}
	// No subcommand: launch the TUI. The runner stack (LimitRunner + ssh
	// BatchMode) is built by domain — one construction site shared with the
	// repo switcher's reRoot (domain.OpenTUI); only the span ring is kept here
	// so the panic dump below can include the session's git spans.
	ring := observ.NewRing(200)
	svc := domain.OpenTUIWithRing(".", ring)
	repo := svc.Repo()
	defer func() {
		if r := recover(); r != nil {
			path := filepath.Join(os.TempDir(), fmt.Sprintf("gg-panic-%d.json", time.Now().Unix()))
			_ = app.DumpRepo(context.Background(), path, repo, ring, []string{fmt.Sprintf("panic: %v", r)})
			fmt.Fprintf(os.Stderr, "gg panicked; debug dump written to %s\n", path)
			panic(r)
		}
	}()
	// Pre-flight: surface the common "not a git repository" / missing-git case as
	// a friendly message instead of launching the TUI only for it to fail with a
	// raw "git status failed (exit 128): fatal: …" dump.
	if _, err := svc.TopLevel(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, friendlyGitError(err))
		os.Exit(1)
	}
	if ef, _, eerr := tui.OpenErrorLog(); eerr == nil && ef != nil {
		observ.SetFailureSink(ef)
		defer func() { observ.SetFailureSink(nil); _ = ef.Close() }()
	}
	if recordPath != "" {
		if err := checkRecordPath(recordPath); err != nil {
			fmt.Fprintln(os.Stderr, "gg: --record:", err)
			os.Exit(2)
		}
	}
	cwd, err := tui.Run(svc, recordPath)
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

// extractRecord pulls the global --record flag (in either "--record path" or
// "--record=path" form) out of args, returning its value and the remaining
// args. A trailing "--record" with no value is dropped.
func extractRecord(args []string) (string, []string) {
	path := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--record":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--record="):
			path = strings.TrimPrefix(a, "--record=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest
}

// checkRecordPath verifies the --record file can be created, so a bad path is
// reported cleanly before the TUI takes over the screen.
func checkRecordPath(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
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

// isVersionRequest reports whether tok asks for the build version, so cmd/gg can
// print buildinfo.String() and exit before touching a repo or launching the TUI.
func isVersionRequest(tok string) bool {
	switch tok {
	case "version", "--version", "-v", "-V":
		return true
	default:
		return false
	}
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
