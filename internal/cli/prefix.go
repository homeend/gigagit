package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/template"
	"github.com/homeend/gigagit/internal/worktree"
)

// cmdPrefix implements `gg prefix <ls|add|rm> ...`: the writable two-scope
// registry of branch-name prefixes (skeletons) selectable at create time.
func cmdPrefix(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg prefix <ls|add|rm|resolve> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ls", "list":
		return prefixList(svc, rest, stdout, stderr)
	case "add":
		return prefixAdd(svc, rest, stdout, stderr)
	case "rm", "remove":
		return prefixRemove(svc, rest, stdout, stderr)
	case "resolve":
		return prefixResolve(svc, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "prefix: unknown subcommand %q\n", sub)
		return 2
	}
}

func prefixList(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if err := flag.NewFlagSet("prefix ls", flag.ContinueOnError).Parse(args); err != nil {
		return 2
	}
	ps, err := svc.Prefixes(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, p := range ps {
		fmt.Fprintf(stdout, "%s\t%s\t%s\n", p.ID, p.Scope.String(), p.Value)
	}
	return 0
}

func prefixAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prefix add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	global := fs.Bool("global", false, "store in the global (every-repo) scope")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg prefix add <value> [--global]")
		return 2
	}
	scope := model.ProfileScopeRepo
	if *global {
		scope = model.ProfileScopeGlobal
	}
	stored, err := svc.AddPrefix(context.Background(), model.Prefix{Value: fs.Arg(0), Scope: scope})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, stored.Value)
	return 0
}

func prefixRemove(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prefix rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	global := fs.Bool("global", false, "remove from the global scope (default: repo)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg prefix rm <value> [--global]")
		return 2
	}
	id := domain.PrefixID(fs.Arg(0))
	scope := model.ProfileScopeRepo
	if *global {
		scope = model.ProfileScopeGlobal
	}
	if err := svc.RemovePrefix(context.Background(), scope, id); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// labelValues is a repeatable --set label=value.
type labelValues map[string]string

func (l labelValues) String() string { return "" }
func (l labelValues) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return fmt.Errorf("want label=value, got %q", v)
	}
	l[k] = val
	return nil
}

const prefixResolveUsage = "usage: gg prefix resolve (<id> | --template <value>) [--set label=value]... [--parent <branch>] [--bump]"

// prefixResolve prints a prefix resolved exactly as the TUI's prefix picker
// does (worktree.ResolvePrefix). Read-only unless --bump, which advances the
// prefix's <seq> counters — the TUI's bump on create — before printing.
func prefixResolve(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prefix resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	inputs := labelValues{}
	fs.Var(inputs, "set", "fill a <user:LABEL>: label=value (repeatable)")
	tmpl := fs.String("template", "", "resolve this template instead of a stored prefix")
	parent := fs.String("parent", "", "the <parent-branch> value (default: the current branch)")
	bump := fs.Bool("bump", false, "advance the prefix's <seq> counters (use once, for the name you create)")
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return 2
		}
		if fs.NArg() == 0 {
			break
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
	if len(pos) > 1 || (len(pos) == 1) == (*tmpl != "") {
		fmt.Fprintln(stderr, prefixResolveUsage)
		return 2
	}
	ctx := context.Background()
	value := *tmpl
	if value == "" {
		ps, err := svc.Prefixes(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		var hits []model.Prefix
		for _, p := range ps {
			if p.ID == pos[0] && (len(hits) == 0 || hits[0].Value != p.Value) {
				hits = append(hits, p)
			}
		}
		switch len(hits) {
		case 0:
			fmt.Fprintf(stderr, "prefix resolve: no prefix %q (gg prefix ls lists the ids)\n", pos[0])
			return 2
		case 1:
			value = hits[0].Value
		default:
			fmt.Fprintf(stderr, "prefix resolve: %q names a repo and a global prefix — pass --template with the one you mean\n", pos[0])
			return 2
		}
	}
	var missing []string
	for _, l := range template.UserLabels(value) {
		if _, ok := inputs[l]; !ok {
			missing = append(missing, "--set "+l+"=…")
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(stderr, "prefix resolve: %s needs %s\n", value, strings.Join(missing, " "))
		return 2
	}
	if *parent == "" && strings.Contains(value, "<parent-branch>") {
		if *parent, _ = svc.CurrentBranch(ctx); *parent == "" {
			fmt.Fprintln(stderr, "prefix resolve: HEAD is detached — pass --parent <branch>")
			return 2
		}
	}
	gitCommonDir, err := svc.GitCommonDir(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	mainTop, _ := svc.TopLevel(ctx)
	if wts, werr := svc.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		mainTop = wts[0].Path // <repo> anchors on the main worktree, as everywhere
	}
	tctx := template.Ctx{
		ParentBranch: *parent,
		Repo:         worktree.RepoName(mainTop),
		Now:          clock.Now,
		Rand:         rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	name, seqs, err := worktree.ResolvePrefix(value, inputs, tctx, gitCommonDir)
	if err != nil {
		fmt.Fprintln(stderr, "prefix resolve:", err)
		return 2
	}
	if *bump {
		for _, n := range seqs {
			if _, err := config.BumpSeq(gitCommonDir, n); err != nil {
				fmt.Fprintln(stderr, "prefix resolve: could not advance <seq:"+n+">:", err)
				return 1
			}
		}
	}
	fmt.Fprintln(stdout, name)
	return 0
}
