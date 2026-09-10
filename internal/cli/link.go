package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// linkUsage is printed for every usage error of `gg link`. The quoting note
// is load-bearing: '#' starts a comment in every POSIX shell, so an unquoted
// hunk link silently loses its hunk. gg deliberately applies no heuristic —
// it says so here instead.
const linkUsage = "usage: gg link [<path>[:<line>]] [--cached | --rev <commit>]\n" +
	"       gg link resolve <gg://…> [--json]\n" +
	"quote links that carry #<hunk> — an unquoted # starts a shell comment"

// cmdLink is `gg link`: print a portable gg:// address, or resolve one.
func cmdLink(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	return runLink(RepoStatePath, svc, args, stdout, stderr)
}

// runLink is cmdLink with the repo registry as a parameter, so tests point it
// at a t.TempDir() file and stay parallel (the runSession seam).
func runLink(statePath string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "resolve" {
		return linkResolve(statePath, svc, args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("link", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cached := fs.Bool("cached", false, "address the staged diff (HEAD → index)")
	rev := fs.String("rev", "", "address a commit's own change (parent → commit)")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) > 1 {
		fmt.Fprintf(stderr, "link: unexpected argument %q\n%s\n", pos[1], linkUsage)
		return 2
	}
	if *cached && *rev != "" {
		fmt.Fprintf(stderr, "link: --cached and --rev are mutually exclusive\n%s\n", linkUsage)
		return 2
	}
	arg := ""
	if len(pos) == 1 {
		arg = pos[0]
	}
	l, err := buildLink(context.Background(), svc, arg, *cached, *rev)
	if err != nil {
		if errors.Is(err, model.ErrLink) {
			fmt.Fprintf(stderr, "link: %v\n%s\n", err, linkUsage)
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, l.String())
	return 0
}

// buildLink assembles the link for one place in THIS repository. pathArg is
// the repo-relative path, optionally suffixed ":<line>", ":old:<line>" or
// "#<hunk>" — parsed by ParseLink itself (against a throwaway repo segment)
// so `gg link` can never disagree with the grammar it prints.
func buildLink(ctx context.Context, svc *domain.Service, pathArg string, cached bool, rev string) (model.Link, error) {
	var l model.Link
	l.Side = model.NoteSideNew
	if s := strings.TrimSpace(pathArg); s != "" {
		probe, err := model.ParseLink(model.LinkScheme + "x/" + strings.TrimPrefix(filepath.ToSlash(s), "./"))
		if err != nil {
			return model.Link{}, err
		}
		if !model.LinkPathOK(probe.Path) {
			return model.Link{}, fmt.Errorf("%w: path contains @, : or # — no gg link", model.ErrLink)
		}
		l.Path, l.Side, l.Line, l.Hunk = probe.Path, probe.Side, probe.Line, probe.Hunk
	}

	switch {
	case cached:
		l.Target = model.LinkTarget{State: model.StateStaged}
	case rev != "":
		if strings.Contains(rev, "..") {
			return model.Link{}, fmt.Errorf("%w: --rev names one commit, not a range", model.ErrLink)
		}
		full, found, err := svc.ResolveRev(ctx, rev)
		if err != nil {
			return model.Link{}, err
		}
		if !found {
			return model.Link{}, fmt.Errorf("unknown revision %q", rev)
		}
		// A producer always writes the FULL sha, so the link cannot become
		// ambiguous as history grows (spec §1).
		l.Target = model.LinkTarget{State: model.StateCommitted, Commit: strings.TrimSpace(full)}
	default:
		l.Target = model.LinkTarget{State: model.StateUnstaged}
	}

	name, err := svc.RepoName(ctx)
	if err != nil {
		return model.Link{}, err
	}
	if name != "" {
		l.Repo = model.LinkRepo{Name: name}
		return l, nil
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return model.Link{}, err
	}
	l.Repo = model.LinkRepo{Abs: filepath.ToSlash(filepath.Clean(top))}
	return l, nil
}

// wireResolvedLink is `gg link resolve --json`'s payload. English protocol
// values throughout — the state words are model.FileState.String()'s.
type wireResolvedLink struct {
	Checkout string `json:"checkout"`
	State    string `json:"state"`
	Path     string `json:"path,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Side     string `json:"side,omitempty"`
	Line     int    `json:"line,omitempty"`
	Hunk     int    `json:"hunk,omitempty"`
}

// linkResolve is `gg link resolve <link> [--json]`: which checkout on THIS
// machine the link names, and the address inside it.
func linkResolve(statePath string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("link resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the resolution as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, linkUsage)
		return 2
	}
	if !isLinkArg(pos[0]) {
		fmt.Fprintf(stderr, "link resolve: %q is not a gg link (it must start with %s)\n", pos[0], model.LinkScheme)
		return 2
	}
	l, err := model.ParseLink(pos[0])
	if err != nil {
		fmt.Fprintln(stderr, "link resolve:", err)
		return 2
	}
	res, err := domain.ResolveLink(context.Background(), l, linkResolveOpts(statePath, svc))
	if err != nil {
		fmt.Fprintln(stderr, "link resolve:", err)
		return 1
	}
	if *asJSON {
		w := wireResolvedLink{
			Checkout: res.Checkout, State: res.Addr.State.String(), Path: res.Addr.Path,
			Commit: res.Addr.Commit, Worktree: res.Addr.Worktree,
			Side: string(res.Side), Line: res.Line, Hunk: res.Hunk,
		}
		if err := json.NewEncoder(stdout).Encode(w); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stdout, res.Checkout)
	line := res.Addr.State.String()
	if res.Addr.Commit != "" {
		line += " " + res.Addr.Commit
	}
	if res.Addr.Path != "" {
		line += " " + res.Addr.Path
	}
	switch {
	case res.Hunk > 0:
		line += fmt.Sprintf(" hunk %d", res.Hunk)
	case res.Line > 0:
		line += fmt.Sprintf(" %s:%d", res.Side, res.Line)
	}
	fmt.Fprintln(stdout, line)
	return 0
}

// isLinkArg reports whether a positional is a gg link. The rule is exactly
// "it starts with gg://" — everything else keeps today's parsing, so
// `gg show <commit>` and `gg diff <rev>` are untouched.
func isLinkArg(s string) bool { return strings.HasPrefix(s, model.LinkScheme) }

// resolveLinkArg parses and resolves a link positional for a consumer verb.
func resolveLinkArg(ctx context.Context, svc *domain.Service, s string) (domain.Resolved, error) {
	l, err := model.ParseLink(s)
	if err != nil {
		return domain.Resolved{}, err
	}
	return domain.ResolveLink(ctx, l, linkResolveOpts(RepoStatePath, svc))
}

// linkResolveOpts wires the resolver to this process: the MRU registry, the
// cwd's service, and the steer-presence probe. domain must not import
// internal/steer, so liveness arrives as a function.
func linkResolveOpts(statePath string, svc *domain.Service) domain.ResolveOpts {
	return domain.ResolveOpts{
		RegistryPath: statePath,
		Cwd:          svc,
		LiveFn: func(commonDir, checkout string) bool {
			dir := config.SessionSteerDir(commonDir, checkout)
			if dir == "" {
				return false
			}
			if _, ok := steer.Live(dir, steer.TUIPresence); ok {
				return true
			}
			_, ok := steer.Live(dir, steer.WebPresence)
			return ok
		},
	}
}
