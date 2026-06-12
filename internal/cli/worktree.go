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

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/template"
	"github.com/gigagit/gg/internal/worktree"
)

// cmdWorktree dispatches `gg worktree <sub>`.
func cmdWorktree(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg worktree <list|add|remove> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdWorktreeList(repo, stdout, stderr)
	case "add":
		return cmdWorktreeAdd(repo, args[1:], stdin, stdout, stderr, cwdFile)
	case "remove":
		return cmdWorktreeRemove(repo, args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "worktree: unknown subcommand %q (use list, add, or remove)\n", args[0])
		return 2
	}
}

func cmdWorktreeList(repo *repoT, stdout, stderr io.Writer) int {
	wts, err := repo.Worktrees(context.Background())
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

func cmdWorktreeAdd(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	ctxBg := context.Background()

	// Start point: explicit arg, else the current branch.
	startPoint := ""
	if len(args) > 0 {
		startPoint = args[0]
	}
	if startPoint == "" {
		cur, err := repo.CurrentBranch(ctxBg)
		if err != nil || cur == "" {
			fmt.Fprintln(stderr, "worktree add: cannot determine current branch; pass a start-point")
			return 2
		}
		startPoint = cur
	}

	top, err := repo.TopLevel(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	gitCommonDir, err := repo.GitCommonDir(ctxBg)
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

	// Prompt stdin for each <user:LABEL>. Prompts go to stderr so stdout stays
	// clean for scripting.
	inputs := map[string]string{}
	reader := bufio.NewReader(stdin)
	for _, label := range tm.Labels() {
		fmt.Fprintf(stderr, "%s: ", label)
		line, _ := reader.ReadString('\n')
		inputs[label] = strings.TrimRight(line, "\r\n")
	}

	seqNames := tm.SeqNames()
	ctx := template.Ctx{
		ParentBranch: startPoint,
		Repo:         worktree.RepoName(top),
		Seqs:         worktree.PeekSeqs(gitCommonDir, seqNames),
		Now:          time.Now,
		Rand:         rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	branch, path, err := worktree.Resolve(tm, "", inputs, ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	res, err := runOperation(ctxBg, repo,
		engine.CreateWorktree{StartPoint: startPoint, Branch: branch, Path: path},
		cliDecider{}, stderr)
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
func cmdWorktreeRemove(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	withBranch := fs.Bool("with-branch", false, "also delete the worktree's branch")
	force := fs.Bool("force", false, "ignore uncommitted changes and unmerged commits")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "worktree remove: a worktree path is required")
		return 2
	}
	target := fs.Arg(0)

	ctxBg := context.Background()
	wts, err := repo.Worktrees(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	absTarget, _ := filepath.Abs(target)
	// A relative target is also resolved against the repo top level — the same
	// base CreateWorktree resolves its repo-relative path template against —
	// so the template-form path (e.g. "../wt/wt-main") works regardless of the
	// process working directory (in-process frontends pass workdir explicitly).
	fromTop := ""
	if !filepath.IsAbs(target) {
		if top, err := repo.TopLevel(ctxBg); err == nil {
			fromTop = filepath.Clean(filepath.Join(top, target))
		}
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
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}

	res, err := runOperation(ctxBg, repo,
		engine.RemoveWorktree{Path: match.Path, Branch: match.Branch}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
