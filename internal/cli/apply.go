package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdApply implements `gg apply [--am | --working] <path>`. Flags precede the
// positional (the gg review convention). The CLI always passes an explicit
// mode — engine.ApplyModeAuto (and its mailbox decision) is TUI-only, so
// `gg apply` never forks mid-run. Default (no flag) = working-tree mode for
// any patch format: safe, non-committing. --am recreates commits from a
// format-patch mailbox (typed refusal on a plain diff). Exit 0 = applied
// cleanly; 1 = failure OR applied-with-conflicts (conflicts left in tree, the
// `gg merge --on-conflict=keep` convention); 2 = usage.
//
// A relative <path> is resolved against workdir (the directory gg was asked
// to run in, threaded from runOne like cmdReview's), not the process cwd. In
// the real binary the two coincide (cmd/gg passes "." and
// filepath.Join(".", p) == p), but the patch file is read through two
// different lenses — the engine sniffs its head via os (process cwd) while
// git apply/am runs with the repo as its cwd — and only an absolute path
// keeps both reads pointed at the same file when gg runs in-process with an
// explicit workdir (the e2e harness, a future MCP server).
func cmdApply(svc *domain.Service, workdir string, rest []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	am := fs.Bool("am", false, "recreate commits from a format-patch mailbox (git am)")
	working := fs.Bool("working", false, "apply to the working tree (the default)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if (*am && *working) || fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg apply [--am | --working] <path>")
		return 2
	}
	mode := engine.ApplyModeWorkingTree
	if *am {
		mode = engine.ApplyModeCommits
	}
	path := fs.Arg(0)
	if !filepath.IsAbs(path) {
		path = filepath.Join(workdir, path)
	}
	res, err := runOperation(context.Background(), svc,
		engine.ApplyPatch{Path: path, Mode: mode}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
