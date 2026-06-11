package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gigagit/gg/internal/app"
	"github.com/gigagit/gg/internal/cli"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
	"github.com/gigagit/gg/internal/tui"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "inspect" {
		runInspect(args[1:])
		return
	}
	if len(args) > 0 && cli.IsCommand(args[0]) {
		os.Exit(cli.Run(".", args, os.Stdout, os.Stderr))
	}
	// A mistyped/unknown subcommand should error, not silently open the TUI.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(os.Stderr, "gg: unknown command %q\n", args[0])
		fmt.Fprintln(os.Stderr, "commands: status commit pull push switch stash undo inspect (run `gg` with no arguments for the TUI)")
		os.Exit(2)
	}
	// No subcommand: launch the TUI.
	ring := observ.NewRing(200)
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", ".", ring)}
	defer func() {
		if r := recover(); r != nil {
			path := filepath.Join(os.TempDir(), fmt.Sprintf("gg-panic-%d.json", time.Now().Unix()))
			_ = app.DumpRepo(context.Background(), path, repo, ring, []string{fmt.Sprintf("panic: %v", r)})
			fmt.Fprintf(os.Stderr, "gg panicked; debug dump written to %s\n", path)
			panic(r)
		}
	}()
	if err := tui.Run(repo); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
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
