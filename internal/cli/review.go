package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
)

// cmdReview implements `gg review [--tool <name>] [--working] [<rev>|<A..B>]`:
// runs the configured review agent headless over the resolved target, prints
// the captured report to stdout, and persists it via domain.ReviewReport.
// Exit 0 on a produced report, 1 on tool failure/empty report/no review tool
// configured, 2 on a flag/usage error.
//
// Flags must come BEFORE the positional (like `gg log [-n N] [<rev>]`, unlike
// `gg show <commit> [--patch]`): --tool takes a value, and flag.Parse stops
// at the first non-flag argument, so a value-taking flag can't safely be
// partitioned out from after a positional the way show's bool-only --patch
// is (see partitionFlags's doc comment in diff.go).
func cmdReview(svc *domain.Service, workdir string, rest []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(stderr)
	toolName := fs.String("tool", "", "review tool name (from config); default: the only one")
	working := fs.Bool("working", false, "review uncommitted working changes")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if *working && fs.NArg() >= 1 {
		fmt.Fprintln(stderr, "usage: gg review [--tool <name>] [--working] [<rev>|<A..B>]")
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: gg review [--tool <name>] [--working] [<rev>|<A..B>]")
		return 2
	}
	ctx := context.Background()

	// Resolve the target.
	var target domain.ReviewTarget
	switch {
	case *working:
		target = domain.WorkingReviewTarget()
	case fs.NArg() >= 1:
		target = reviewTargetForArg(fs.Arg(0))
	default:
		t, err := svc.BranchReviewTarget(ctx, "HEAD")
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		target = t
	}

	// Pick the review tool command from config.
	cmd, err := selectReviewCommand(svc, *toolName, stderr)
	if err != nil {
		return 1
	}
	resolved, err := template.ResolveCommand(cmd.Command, nil, template.CmdCtx{Range: target.Range, Repo: workdir})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	res, err := svc.ReviewReport(ctx, target, resolved, []string{"GG_TASK=review"}, time.Now())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	io.WriteString(stdout, res.Content)
	if !strings.HasSuffix(res.Content, "\n") {
		io.WriteString(stdout, "\n")
	}
	fmt.Fprintln(stderr, "report:", res.Path)
	return 0
}

// reviewTargetForArg classifies a single positional argument as either an
// explicit range (contains "..", used as-is) or a single commit (reviewed
// against its own parent via "<arg>^..<arg>" — model.DiffSpec{Rev: arg} alone
// would diff the WORKING TREE against arg, which is empty on a clean
// checkout, not the commit's own change).
func reviewTargetForArg(arg string) domain.ReviewTarget {
	if strings.Contains(arg, "..") {
		return domain.ReviewTarget{Kind: domain.ReviewRange, Range: arg, Diff: model.DiffSpec{Rev: arg}}
	}
	rng := arg + "^.." + arg
	return domain.ReviewTarget{Kind: domain.ReviewRange, Range: rng, Diff: model.DiffSpec{Rev: rng}}
}

// selectReviewCommand loads the effective config and returns the chosen
// review-category command: --tool picks by name; exactly one candidate with
// no --tool uses it; zero or more-than-one-without---tool is an error
// (listing names in the ambiguous case).
func selectReviewCommand(svc *domain.Service, name string, stderr io.Writer) (config.ToolCommand, error) {
	cfg, err := loadConfigFor(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error: loading config:", err)
		return config.ToolCommand{}, err
	}
	var cands []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(exttool.CatReview) {
			continue
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, tc.PerFile) != nil {
			continue
		}
		cands = append(cands, tc)
	}
	if len(cands) == 0 {
		fmt.Fprintln(stderr, "error: no review tool configured (see [[tools.command]] category=\"review\")")
		return config.ToolCommand{}, fmt.Errorf("no review tool")
	}
	if name != "" {
		for _, tc := range cands {
			if tc.Name == name {
				return tc, nil
			}
		}
		fmt.Fprintf(stderr, "error: no review tool named %q\n", name)
		return config.ToolCommand{}, fmt.Errorf("no such tool")
	}
	if len(cands) > 1 {
		var names []string
		for _, tc := range cands {
			names = append(names, tc.Name)
		}
		fmt.Fprintf(stderr, "error: multiple review tools; pass --tool (%s)\n", strings.Join(names, ", "))
		return config.ToolCommand{}, fmt.Errorf("ambiguous tool")
	}
	return cands[0], nil
}

// loadConfigFor loads the effective config (global + active repo) for svc's
// repo, mirroring cmdWorktreeAdd's resolution (internal/cli/worktree.go): the
// committed <top>/.gg.toml, overridden by a machine-local private file keyed
// on the MAIN worktree, if one exists. Reuses the caller's already-open
// Service rather than opening a second one.
func loadConfigFor(svc *domain.Service) (config.Config, error) {
	ctx := context.Background()
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return config.Config{}, err
	}
	privatePath := ""
	if wts, werr := svc.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		privatePath = config.PrivateRepoPath(wts[0].Path)
	}
	active := config.ActiveRepoConfigPath(filepath.Join(top, ".gg.toml"), privatePath)
	return config.Load(config.DefaultGlobalPath(), active)
}
