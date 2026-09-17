package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/repos"
)

// RepoStatePath is the repo-switcher registry location. "" disables recording
// and yields an empty registry — cmd/gg wires the real path; tests stay
// hermetic by default.
var RepoStatePath string

// InitHomeDir is the home directory used for `gg init`'s home-scoped agent
// detection. "" skips home-scoped agents — cmd/gg wires the real home; tests
// stay hermetic by default.
var InitHomeDir string

// InitTargetsPath overrides where `gg init` records custom (--to) install
// targets. Empty = the default agent-targets.toml beside repos.toml in the
// state dir. Tests set it to a temp file so they never touch real state.
var InitTargetsPath string

// setupCLIService applies the per-service setup EVERY CLI service needs — the
// one the cwd's service gets in Run, and (since gg links) the one a
// cross-checkout target service gets in openLinkTarget. A link consumer that
// skipped it would render the SAME command's diff differently depending on
// which checkout it ran against, which is exactly what a portable address
// must not do.
func setupCLIService(svc *domain.Service) {
	// The scriptable CLI keeps `gg status` faithful to `git status`: the
	// EOL-only filter is a TUI Files-panel convenience and the CLI has no config
	// to disable it, so a script's output must not silently change.
	svc.SetShowEOLOnlyChanges(true)
	// Best-effort: inject the configured branch-version snapshot policy (see
	// internal/engine's VersionsPolicy). A load error (not a repo, no config
	// files, etc.) silently keeps the domain default (enabled, 90 days) —
	// every other CLI command must keep working even when this fails.
	if cfg, err := loadConfigFor(svc); err == nil {
		svc.SetVersionsPolicy(engine.VersionsPolicy{Enabled: !cfg.Versions.Disabled, MaxAgeDays: cfg.Versions.MaxAgeDays})
	}
}

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
	setupCLIService(svc)
	if err := svc.PreflightRequired(context.Background()); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cmd, rest := args[0], args[1:]
	// Record this repo in the switcher registry (best-effort: errors and
	// non-repo working directories are ignored). Skip for "repo" subcommands
	// since they are registry management commands, not git operations, and may
	// be run from arbitrary directories.
	//
	// The remote name is deliberately "": resolving it costs two extra git
	// invocations, which every one-shot `gg status` would then pay. Touch keeps
	// whatever a TUI/web session already recorded, and domain.ResolveLink
	// backfills an entry that still has none.
	if RepoStatePath != "" && cmd != "repo" {
		if top, err := svc.TopLevel(context.Background()); err == nil {
			_ = repos.Touch(RepoStatePath, top, "", time.Now())
		}
	}
	if cmd == "batch" {
		return cmdBatch(svc, workdir, rest, stdin, stdout, stderr, cwdFile)
	}
	return runOne(svc, workdir, cmd, rest, stdin, stdout, stderr, cwdFile)
}

// runOne dispatches one already-split command against an open service. It
// is shared by Run (one command per process) and cmdBatch (a script of
// commands against one service). stdin is what interactive prompts read;
// batch passes an empty reader so a command can never block on input.
func runOne(svc *domain.Service, workdir, cmd string, rest []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	switch cmd {
	case "status":
		return cmdStatus(svc, stdout, stderr)
	case "commit":
		return cmdCommit(svc, rest, stdout, stderr)
	case "pull":
		return cmdPull(svc, rest, stdin, stdout, stderr)
	case "push":
		return cmdPush(svc, rest, stdin, stdout, stderr)
	case "switch":
		return cmdSwitch(svc, rest, stdout, stderr)
	case "checkout":
		return cmdCheckout(svc, rest, stdout, stderr)
	case "branch":
		return cmdBranch(svc, rest, stdin, stdout, stderr)
	case "stash":
		return cmdStash(svc, workdir, rest, stdout, stderr)
	case "undo":
		return cmdUndo(svc, rest, stdout, stderr)
	case "discard":
		return cmdDiscard(svc, workdir, rest, stdin, stdout, stderr)
	case "add":
		return cmdAdd(svc, workdir, rest, stdin, stdout, stderr)
	case "unstage":
		return cmdUnstage(svc, workdir, rest, stdout, stderr)
	case "apply":
		return cmdApply(svc, workdir, rest, stdout, stderr)
	case "shelf":
		return cmdShelf(svc, workdir, rest, stdin, stdout, stderr)
	case "bookmark":
		return cmdBookmark(svc, workdir, rest, stdin, stdout, stderr)
	case "note":
		return cmdNote(svc, rest, stdin, stdout, stderr)
	case "link":
		return cmdLink(svc, workdir, rest, stdout, stderr)
	case "links":
		return cmdLinks(svc, rest, stdout, stderr)
	case "session":
		return cmdSession(svc, rest, stdout, stderr)
	case "open":
		return cmdOpen(svc, rest, stdout, stderr)
	case "log":
		return cmdLog(svc, rest, stdout, stderr)
	case "prefix":
		return cmdPrefix(svc, rest, stdout, stderr)
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
		return cmdWorktree(svc, workdir, rest, stdin, stdout, stderr, cwdFile)
	case "remote":
		return cmdRemote(svc, rest, stdin, stdout, stderr)
	case "tag":
		return cmdTag(svc, rest, stdin, stdout, stderr)
	case "compare":
		return cmdCompare(RepoStatePath, svc, rest, stdout, stderr)
	case "preview":
		return cmdPreview(svc, rest, stdout, stderr)
	case "diff":
		return cmdDiff(svc, workdir, rest, stdout, stderr)
	case "show":
		return cmdShow(svc, workdir, rest, stdout, stderr)
	case "review":
		return cmdReview(svc, workdir, rest, stdout, stderr)
	case "versions":
		return cmdVersions(svc, rest, stdin, stdout, stderr)
	case "migrate":
		return cmdMigrate(svc, rest, stdout, stderr)
	case "unlock":
		return cmdUnlock(svc, rest, stdout, stderr)
	case "repo":
		return cmdRepo(rest, stdout, stderr, cwdFile)
	case "init":
		return cmdInit(workdir, rest, stdin, stdout, stderr)
	case "skill":
		return cmdSkill(rest, stdout, stderr)
	case "config":
		return cmdConfig(svc, workdir, rest, stdout, stderr)
	case "batch":
		fmt.Fprintln(stderr, "batch: nested batch is not allowed")
		return 2
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		return 2
	}
}

var commands = map[string]bool{
	"status": true, "commit": true, "pull": true, "push": true,
	"switch": true, "checkout": true, "branch": true, "stash": true, "undo": true, "merge": true, "rebase": true, "worktree": true,
	"cherry-pick": true, "revert": true, "reset": true, "fast-forward": true,
	"discard": true, "add": true, "unstage": true, "shelf": true, "bookmark": true, "log": true, "prefix": true,
	"remote": true, "tag": true, "compare": true, "preview": true, "diff": true, "show": true,
	"inspect": true, "repo": true, "init": true, "config": true, "batch": true,
	"review": true, "apply": true, "versions": true, "unlock": true, "migrate": true,
	"note": true, "skill": true, "session": true, "link": true, "links": true, "open": true,
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
		// git status v2 uses '.' for "no change" (in addition to null byte from uninitialized fields)
		if x == 0 || x == '.' {
			x = ' '
		}
		if y == 0 || y == '.' {
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
	if len(args) > 0 && args[0] == "export-patch" {
		return cmdCommitExportPatch(svc, args[1:], stdout, stderr)
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

// printDrift reports domain.DriftAfter's findings for branch, following a
// rebase/merge/pull that SUCCEEDED AND CHANGED SOMETHING. Callers gate on
// `code == 0 && res.Changed`, not the exit code alone: finish returns 0
// whenever err == nil, and an answered --on-conflict=abort returns
// Result{Changed:false}, nil — the pre-op snapshot is already written and the
// tip never moved, so an abort would report every path the other side
// contributed as D. An op that errored or left a conflict paused never
// reaches here either. Per the
// spec, a frontend that could resume a paused op into completion would also
// report drift on that path; the CLI has no resume verb (a conflicted `gg
// rebase`/`gg merge`/`gg pull` returns with the conflict still unresolved,
// so the invocation that would print a summary never happens), so only the
// drift rule applies here: print when the operation itself changed the
// branch's change set (Report.Drifted(), i.e. Report.Added is non-empty).
// Report.Removed is never alarming (upstream absorbed it) and prints
// quietly underneath. DriftAfter returning Checked == false (nothing
// recorded to compare, or the versions feature is off) prints nothing.
func printDrift(ctx context.Context, svc *domain.Service, branch string, stdout io.Writer) {
	if branch == "" {
		return
	}
	drift, err := svc.DriftAfter(ctx, branch)
	if err != nil || !drift.Checked || !drift.Report.Drifted() {
		return
	}
	fmt.Fprintf(stdout, "! this operation changed %s's change set:\n", branch)
	for _, e := range drift.Report.Added {
		fmt.Fprintf(stdout, "  %c %s\n", e.Status, e.Path)
	}
	for _, e := range drift.Report.Removed {
		fmt.Fprintf(stdout, "  (absorbed upstream: %c %s)\n", e.Status, e.Path)
	}
}
