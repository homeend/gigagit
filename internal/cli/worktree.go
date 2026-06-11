package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/template"
	"github.com/gigagit/gg/internal/worktree"
)

// cmdWorktree dispatches `gg worktree <sub>`.
func cmdWorktree(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg worktree <list|add> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdWorktreeList(repo, stdout, stderr)
	case "add":
		return cmdWorktreeAdd(repo, args[1:], stdin, stdout, stderr, cwdFile)
	default:
		fmt.Fprintf(stderr, "worktree: unknown subcommand %q (use list or add)\n", args[0])
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
