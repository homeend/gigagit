package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
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
// workdir is the directory gg was asked to run in (threaded from runOne like
// cmdApply's), not the process cwd — a relative <path> argument is resolved
// against it, then rebased onto the checkout top level.
func cmdLink(svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer) int {
	return runLink(RepoStatePath, svc, workdir, args, stdout, stderr)
}

// runLink is cmdLink with the repo registry as a parameter, so tests point it
// at a t.TempDir() file and stay parallel (the runSession seam).
func runLink(statePath string, svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer) int {
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
	l, err := buildLink(context.Background(), svc, workdir, arg, *cached, *rev)
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
// the path as the user typed it — relative to workdir (the directory gg was
// asked to run in, not the process cwd) unless already absolute — optionally
// suffixed ":<line>", ":old:<line>" or "#<hunk>". The suffix is split off by
// ParseLink itself (against a throwaway repo segment) so `gg link` can never
// disagree with the grammar it prints; the bare path portion is then rebased
// onto the checkout top level, since the link grammar's <path> is always
// top-level-relative (spec), never cwd-relative.
func buildLink(ctx context.Context, svc *domain.Service, workdir, pathArg string, cached bool, rev string) (model.Link, error) {
	var l model.Link
	l.Side = model.NoteSideNew

	// TopLevel is needed both to rebase a path argument and, when this repo
	// has no remote, as the link's own local-form identity — fetch it once
	// up front so both uses share the one git invocation.
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return model.Link{}, err
	}

	if s := strings.TrimSpace(pathArg); s != "" {
		raw := strings.TrimPrefix(filepath.ToSlash(s), "./")
		// A separator in the PATH is checked here, on the raw argument, so the
		// user gets the intended message: run through ParseLink first and an
		// '@' or a stray '#' surfaces as the grammar's "target must be…" /
		// "hunk must be…" prose about a link the user never typed. ':' is NOT
		// checked here — it is the line suffix, and on Windows it is also the
		// drive colon of a perfectly good absolute argument; LinkPathOK gets
		// the REBASED, top-level-relative path below, which has neither.
		if strings.ContainsRune(raw, '@') || !linkHunkSuffixOK(raw) {
			return model.Link{}, fmt.Errorf("%w: path contains @, : or # — no gg link", model.ErrLink)
		}
		probe, err := probeLinkArg(raw)
		if err != nil {
			return model.Link{}, err
		}
		rel, err := rebaseLinkPath(top, workdir, probe.Repo.Abs)
		if err != nil {
			return model.Link{}, err
		}
		if !model.LinkPathOK(rel) {
			return model.Link{}, fmt.Errorf("%w: path contains @, : or # — no gg link", model.ErrLink)
		}
		if rel == "" && (probe.Line > 0 || probe.Hunk > 0) {
			return model.Link{}, fmt.Errorf("%w: a line or a hunk needs a file path", model.ErrLink)
		}
		l.Path, l.Side, l.Line, l.Hunk = rel, probe.Side, probe.Line, probe.Hunk
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
	abs := filepath.ToSlash(filepath.Clean(top))
	// The local form carries the CHECKOUT path, which is no more expressible
	// than a file path is: a checkout under /home/user@corp or /mnt/backup#1
	// would emit a link ParseLink refuses (the first '@' is the target
	// separator, the first '#' the hunk one). Refuse to print it instead.
	if !model.LinkAbsOK(abs) {
		return model.Link{}, fmt.Errorf("%w: this repository has no remote and its checkout path %q contains @ or # — no gg link", model.ErrLink, abs)
	}
	l.Repo = model.LinkRepo{Abs: abs}
	return l, nil
}

// linkProbePrefix is the throwaway checkout segment probeLinkArg parses the
// argument against. The LOCAL form is used deliberately: its repo half holds
// the path undivided and is never LinkPathOK-checked, so a Windows absolute
// argument's drive colon ("C:/repo/a.txt") survives the probe — the
// remote-named form now refuses one (it cannot round-trip there).
const linkProbePrefix = "/gg-link-probe/"

// probeLinkArg splits a `gg link` path argument into its path and its
// ":<line>" / ":old:<line>" / "#<hunk>" suffix using ParseLink itself, so
// `gg link` can never disagree with the grammar it prints. The returned
// Link's Repo.Abs holds the path portion (the local form's shape); nothing
// else about the returned value is meaningful.
func probeLinkArg(raw string) (model.Link, error) {
	l, err := model.ParseLink(model.LinkScheme + linkProbePrefix + raw)
	if err != nil {
		return model.Link{}, err
	}
	l.Repo.Abs = strings.TrimPrefix(l.Repo.Abs, linkProbePrefix)
	return l, nil
}

// linkHunkSuffixOK reports whether every '#' in a `gg link` argument is the
// grammar's own hunk suffix — i.e. there is at most one and a positive number
// follows it. A '#' inside a file NAME is not expressible in a link, and this
// is what lets buildLink say so in its own words instead of letting the parser
// complain about a hunk the user never wrote.
func linkHunkSuffixOK(raw string) bool {
	i := strings.IndexByte(raw, '#')
	if i < 0 {
		return true
	}
	n, err := strconv.Atoi(raw[i+1:])
	return err == nil && n >= 1
}

// rebaseLinkPath turns p — the raw path portion of a `gg link` argument,
// relative to workdir (the directory gg was asked to run in) unless already
// absolute — into the checkout-top-level-relative git slash path the link
// grammar requires. "" (no path argument) passes through unchanged. Refuses
// (wrapping model.ErrLink) a path that resolves outside top, so a link never
// silently addresses the wrong file when gg runs from a subdirectory.
func rebaseLinkPath(top, workdir, p string) (string, error) {
	if p == "" {
		return "", nil
	}
	native := filepath.FromSlash(p)
	abs := native
	if !filepath.IsAbs(native) {
		// workdir is what cmd/gg passed to cli.Run, which is "." for the real
		// binary (every other verb is happy with that) — make it absolute here
		// or filepath.Rel below fails outright against the ABSOLUTE top level,
		// and `gg link a.txt` never works outside a test.
		base := workdir
		if a, err := filepath.Abs(base); err == nil {
			base = a
		}
		abs = filepath.Join(base, native)
	}
	rel, err := filepath.Rel(top, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%w: path %q is outside the checkout", model.ErrLink, p)
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
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
		// ResolveLink wraps model.ErrLink for a MALFORMED link that got past
		// the parser (the empty-commit guard, the path-escape refusal), and
		// those are exit 2 in every other verb — linkExit is the shared rule.
		return linkExit("link resolve", err, stderr)
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
