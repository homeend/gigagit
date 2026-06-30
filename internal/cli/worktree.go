package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/worktree"
)

// cmdWorktree dispatches `gg worktree <sub>`.
func cmdWorktree(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg worktree <list|add|remove> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdWorktreeList(svc, stdout, stderr)
	case "add":
		return cmdWorktreeAdd(svc, args[1:], stdin, stdout, stderr, cwdFile)
	case "remove":
		return cmdWorktreeRemove(svc, args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "worktree: unknown subcommand %q (use list, add, or remove)\n", args[0])
		return 2
	}
}

func cmdWorktreeList(svc *domain.Service, stdout, stderr io.Writer) int {
	wts, err := svc.Worktrees(context.Background())
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

func cmdWorktreeAdd(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	fs := flag.NewFlagSet("worktree add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	forBranch := fs.String("branch", "", "create the worktree for this existing branch (no new branch)")
	noHook := fs.Bool("no-hook", false, "skip the configured [worktree] post_create_hook")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	args = fs.Args()

	ctxBg := context.Background()

	if *forBranch != "" && len(args) > 0 {
		fmt.Fprintln(stderr, "worktree add: --branch and a start-point are mutually exclusive (the branch is the source)")
		return 2
	}

	// Start point: explicit arg, else the current branch. With --branch the
	// branch itself plays the <parent-branch> role for the path template.
	startPoint := *forBranch
	if startPoint == "" {
		if len(args) > 0 {
			startPoint = args[0]
		}
		if startPoint == "" {
			cur, err := svc.CurrentBranch(ctxBg)
			if err != nil || cur == "" {
				fmt.Fprintln(stderr, "worktree add: cannot determine current branch; pass a start-point")
				return 2
			}
			startPoint = cur
		}
	}

	top, err := svc.TopLevel(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	gitCommonDir, err := svc.GitCommonDir(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	cfg, err := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml"))
	if err != nil {
		fmt.Fprintln(stderr, "error: loading config:", err)
		return 1
	}

	tm := worktree.Templates{
		Branch: cfg.Worktree.DefaultBranchTemplate,
		Path:   cfg.Worktree.PathTemplate,
	}
	if *forBranch != "" {
		tm = worktree.Templates{Path: cfg.Worktree.PathTemplate} // branch template bypassed
	}

	// Prompt stdin for each <user:LABEL>. Prompts go to stderr so stdout stays
	// clean for scripting.
	inputs := map[string]string{}
	reader := bufio.NewReader(stdin)
	for _, label := range tm.Labels() {
		fmt.Fprintf(stderr, "%s: ", label)
		line, _ := reader.ReadString('\n')
		inputs[label] = strings.TrimRight(line, "\r\n")
	}

	// The <repo> token and the relative-path base both anchor on the MAIN
	// worktree (git lists it first), not the current one — otherwise running
	// from a linked worktree nests the new worktree under it (doubled
	// ".worktrees"). The engine resolves the relative path the same way.
	mainTop := top
	if wts, werr := svc.Worktrees(ctxBg); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		mainTop = wts[0].Path
	}

	seqNames := tm.SeqNames()
	ctx := template.Ctx{
		ParentBranch: startPoint,
		Repo:         worktree.RepoName(mainTop),
		Seqs:         worktree.PeekSeqs(gitCommonDir, seqNames),
		Now:          time.Now,
		Rand:         rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	branch, path, err := worktree.Resolve(tm, *forBranch, inputs, ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	hook := cfg.Worktree.PostCreateHook
	if *noHook {
		hook = ""
	}
	var op engine.Operation = engine.CreateWorktree{StartPoint: startPoint, Branch: branch, Path: path, PostCreateHook: hook}
	if *forBranch != "" {
		op = engine.CreateWorktreeForBranch{Branch: branch, Path: path, PostCreateHook: hook}
	}
	res, err := runOperation(ctxBg, svc, op, cliDecider{}, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	// Consume the counters the templates used, now that creation succeeded.
	for _, name := range seqNames {
		_, _ = config.BumpSeq(gitCommonDir, name)
	}

	fmt.Fprintf(stdout, "✓ created worktree %s at %s\n", branch, res.Path)
	if cwdFile != "" && res.Path != "" {
		_ = os.WriteFile(cwdFile, []byte(res.Path), 0o644)
	}
	return 0
}

// cmdWorktreeRemove implements `gg worktree remove [--with-branch] [--force] <path>`.
// Flags must precede the path. --with-branch also deletes the branch;
// --force ignores uncommitted changes and unmerged commits.
func cmdWorktreeRemove(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	withBranch := fs.Bool("with-branch", false, "also delete the worktree's branch")
	force := fs.Bool("force", false, "ignore uncommitted changes and unmerged commits; unlock a locked worktree")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "worktree remove: a worktree path is required")
		return 2
	}
	target := fs.Arg(0)

	ctxBg := context.Background()
	wts, err := svc.Worktrees(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	absTarget, _ := filepath.Abs(target)
	// A relative target is resolved against the MAIN worktree root — the same
	// base CreateWorktree resolves its repo-relative path template against (git
	// lists the main worktree first) — so the template-form path
	// (e.g. "../wt/wt-main") round-trips regardless of the process working
	// directory or which linked worktree gg runs from.
	fromTop := ""
	if !filepath.IsAbs(target) && len(wts) > 0 && wts[0].Path != "" {
		fromTop = filepath.Clean(filepath.Join(wts[0].Path, target))
	}
	var match *model.Worktree
	for i := range wts {
		if wts[i].Path == target || wts[i].Path == absTarget ||
			(fromTop != "" && wts[i].Path == fromTop) {
			match = &wts[i]
			break
		}
	}
	if match == nil {
		fmt.Fprintf(stderr, "worktree remove: no worktree at %q\n", target)
		return 1
	}

	policy := map[string]string{"remove-scope": "worktree-only"}
	if *withBranch {
		policy["remove-scope"] = "worktree-and-branch"
	}
	if *force {
		policy["worktree-dirty"] = "force"
		policy["branch-unmerged"] = "force-delete"
		policy["worktree-locked"] = "unlock-and-remove"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}

	res, err := runOperation(ctxBg, svc,
		engine.RemoveWorktree{Path: match.Path, Branch: match.Branch}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
