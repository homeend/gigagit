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

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/model"
)

// linkUsage is printed for every usage error of `gg link`. The quoting note
// is load-bearing: '#' starts a comment in every POSIX shell, so an unquoted
// hunk link silently loses its hunk. gg deliberately applies no heuristic —
// it says so here instead.
const linkUsage = "usage: gg link [<path>[:<line>]] [--cached | --rev <commit> | --preview <id|label|<target>...<source>> | --ref <branch|tag> | --pair <a>..<b>] [--bookmark <id> | --shelf <id>]\n" +
	"       gg link resolve <gg://…> [--json]\n" +
	"       gg links [--json]  (this repo's copied-link history)\n" +
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
	ref := fs.String("ref", "", "address a branch or tag TIP (unbounded: the whole tree there)")
	pair := fs.String("pair", "", "address a CHANGE-SET, <a>..<b> (bounded: what it changed)")
	bookmark := fs.String("bookmark", "", "attach a ?bookmark=<id> landing hint")
	shelf := fs.String("shelf", "", "attach a ?shelf=<id> landing hint")
	pf := addPreviewFlag(fs)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) > 1 {
		fmt.Fprintf(stderr, "link: unexpected argument %q\n%s\n", pos[1], linkUsage)
		return 2
	}
	// --cached, --rev, --preview, --ref and --pair all name the TARGET, so at
	// most one may be set. A count, not a web of pairwise checks: the pairwise
	// form was already two checks for two flags, and five flags would be ten.
	set := 0
	for _, on := range []bool{*cached, *rev != "", pf.set(), *ref != "", *pair != ""} {
		if on {
			set++
		}
	}
	if set > 1 {
		// --preview keeps its own shared message when it is one of the two, so
		// `gg link --preview x --rev y` reads the same as `gg diff` does.
		if pf.set() {
			return previewUsageErr("link", stderr)
		}
		fmt.Fprintf(stderr, "link: --cached, --rev, --preview, --ref and --pair name the target; use one\n%s\n", linkUsage)
		return 2
	}
	// The two hints are a LANDING, and a link lands in one place.
	if *bookmark != "" && *shelf != "" {
		fmt.Fprintf(stderr, "link: --bookmark and --shelf are mutually exclusive\n%s\n", linkUsage)
		return 2
	}
	hint := model.LinkHint{}
	switch {
	case *bookmark != "":
		hint = model.LinkHint{Kind: "bookmark", ID: *bookmark}
	case *shelf != "":
		hint = model.LinkHint{Kind: "shelf", ID: *shelf}
	}
	arg := ""
	if len(pos) == 1 {
		arg = pos[0]
	}
	ctx := context.Background()
	var prev *model.LinkPreview
	if pf.set() {
		// Resolve the argument (id | label | <target>...<source>) to the pair's
		// NAMES, and refuse a pair that cannot be shown — a link nobody can open
		// is worse than no link.
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		if !model.LinkRefOK(tgt.Source) || !model.LinkRefOK(tgt.Target) {
			fmt.Fprintf(stderr, "link: %s...%s cannot be expressed in a gg link (a branch or tag name may not contain @, :, #, ? or whitespace)\n", tgt.Target, tgt.Source)
			return 1
		}
		prev = &model.LinkPreview{Source: tgt.Source, Target: tgt.Target}
	}
	l, err := buildLink(ctx, svc, workdir, arg, linkOpts{
		Cached: *cached, Rev: *rev, Ref: *ref, Pair: *pair, Preview: prev, Hint: hint,
	})
	if err != nil {
		if errors.Is(err, model.ErrLink) {
			fmt.Fprintf(stderr, "link: %v\n%s\n", err, linkUsage)
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	// Best-effort, after the link is known good: a history that cannot be
	// written must never fail the copy the user asked for, and must never
	// change what gets printed or the exit code.
	kind, id, subject := linkRecordFields(ctx, svc, l)
	svc.RecordLink(ctx, l.String(), linkDesc(kind, id, subject))
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
// linkOpts is what `gg link` was asked to address. At most one target field
// is set (none means the working tree); the hint composes with any of them.
type linkOpts struct {
	Cached  bool
	Rev     string
	Ref     string
	Pair    string
	Preview *model.LinkPreview
	Hint    model.LinkHint
}

func buildLink(ctx context.Context, svc *domain.Service, workdir, pathArg string, o linkOpts) (model.Link, error) {
	var l model.Link
	l.Side = model.NoteSideNew
	l.Hint = o.Hint

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
		//
		// '?' is checked for a STRONGER reason than the other two: ParseLink
		// does not fail on it. It reads everything from the first '?' as the
		// hint and hands back a TRUNCATED path, so without this guard
		// `gg link 'a?bookmark=x.txt'` printed, with exit 0, a link to the
		// unrelated file "a" — LinkPathOK below is only ever asked about the
		// already-shortened path and can never see the '?'. A '?' in the
		// argument is therefore refused outright rather than read as a hint:
		// `gg link`'s hint channel is --bookmark/--shelf, and a filename is
		// not a second spelling of it. (Consequently probe.Hint is always
		// empty here; there is no hint to carry or to drop.)
		if strings.ContainsAny(raw, "@?") || !linkHunkSuffixOK(raw) {
			return model.Link{}, fmt.Errorf("%w: path contains @, :, # or ? — no gg link", model.ErrLink)
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
			return model.Link{}, fmt.Errorf("%w: path contains @, :, # or ? — no gg link", model.ErrLink)
		}
		if rel == "" && (probe.Line > 0 || probe.Hunk > 0) {
			return model.Link{}, fmt.Errorf("%w: a line or a hunk needs a file path", model.ErrLink)
		}
		l.Path, l.Side, l.Line, l.Hunk = rel, probe.Side, probe.Line, probe.Hunk
	}

	switch {
	case o.Preview != nil:
		// The preview's old side is the merge base, which no stored address
		// names (spec §1.1): "old:" has nothing to point at.
		if l.Side == model.NoteSideOld {
			return model.Link{}, fmt.Errorf("%w: a merge preview addresses the new side only; drop \"old:\"", model.ErrLink)
		}
		l.Target = model.LinkTarget{State: model.StateCommitted, Preview: o.Preview}
	case o.Cached:
		l.Target = model.LinkTarget{State: model.StateStaged}
	case o.Rev != "":
		if strings.Contains(o.Rev, "..") {
			return model.Link{}, fmt.Errorf("%w: --rev names one commit, not a range", model.ErrLink)
		}
		full, found, err := svc.ResolveRev(ctx, o.Rev)
		if err != nil {
			return model.Link{}, err
		}
		if !found {
			return model.Link{}, fmt.Errorf("unknown revision %q", o.Rev)
		}
		// A producer always writes the FULL sha, so the link cannot become
		// ambiguous as history grows (spec §1).
		l.Target = model.LinkTarget{State: model.StateCommitted, Commit: strings.TrimSpace(full)}
	case o.Ref != "":
		// A ref target keeps the NAME, deliberately: "@ref:main" addresses the
		// branch, not whichever commit it sits on today. The name is the user's
		// own input, so a name the grammar cannot carry is a usage error (exit
		// 2 through ErrLink) rather than the preview arm's exit 1 — nothing was
		// looked up and failed, the argument was simply not expressible.
		if !model.LinkRefOK(o.Ref) {
			return model.Link{}, fmt.Errorf("%w: %q cannot be expressed in a gg link (a branch or tag name may not contain @, :, #, ? or whitespace)", model.ErrLink, o.Ref)
		}
		// Refuse a name git does not have: a link nobody can open is worse
		// than no link, which is the rule the --preview arm already follows.
		// This costs one rev-parse and catches the common typo at the producer
		// instead of on whichever machine opens the link.
		if _, found, err := svc.ResolveRev(ctx, o.Ref); err != nil {
			return model.Link{}, err
		} else if !found {
			return model.Link{}, fmt.Errorf("unknown revision %q", o.Ref)
		}
		l.Target = model.LinkTarget{State: model.StateCommitted, Ref: o.Ref}
	case o.Pair != "":
		a, b, err := splitLinkPair(o.Pair)
		if err != nil {
			return model.Link{}, err
		}
		// BOTH halves become full shas, so the link is portable and
		// self-describing (spec §3.2): a change-set spelled with branch names
		// would mean something different on another machine, and something
		// different HERE tomorrow.
		fullA, err := resolveLinkPairHalf(ctx, svc, a)
		if err != nil {
			return model.Link{}, err
		}
		fullB, err := resolveLinkPairHalf(ctx, svc, b)
		if err != nil {
			return model.Link{}, err
		}
		l.Target = model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: fullA, B: fullB}}
	default:
		l.Target = model.LinkTarget{State: model.StateUnstaged}
	}

	// ONE derivation of the repo half, shared with every other producer (the
	// previews migration composes links nobody typed): two would be two arms
	// that must agree and eventually would not.
	repo, err := svc.LinkRepo(ctx)
	if err != nil {
		return model.Link{}, err
	}
	l.Repo = repo
	return l, linkRoundTrips(l)
}

// linkRoundTrips refuses a link `gg link` would print but ParseLink could not
// read back. Every OTHER field is already screened against the grammar by a
// purpose-built check (LinkPathOK, LinkAbsOK, LinkRefOK), but a HINT id is the
// user's own free-form argument and parseLinkHint rejects a separator in it, so
// this is the cheapest way to hold `gg link`'s standing promise: everything it
// prints parses. Wrapping ErrLink makes it a usage error, which is what a bad
// argument is.
func linkRoundTrips(l model.Link) error {
	s := l.String()
	if _, err := model.ParseLink(s); err != nil {
		return fmt.Errorf("%w: %s is not a readable gg link (%v)", model.ErrLink, s, err)
	}
	return nil
}

// descMax caps the free-text portion of a stored Desc — a commit subject or
// a bookmark/shelf label is otherwise unbounded — so one row of `gg links`
// stays one line.
const descMax = 60

// truncateDesc bounds s to descMax runes, trimming surrounding whitespace
// first. Rune-safe: cutting mid-multibyte-character would corrupt the tail.
func truncateDesc(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= descMax {
		return s
	}
	return string(r[:descMax])
}

// linkDesc is the human label stored with a copied link (ruling R7: captured
// at creation, never derived at read time — the describing context, which
// row the user was on, is gone by the time anything lists this). The forms
// are spec §4.3's table:
//
//	branch:   <name>
//	bookmark: <label>
//	shelf:    <label>
//	preview:  <target>...<source>
//	commit:   <short> <subject>
//	stash:    <subject>
//	file:     <path>
//
// commit and stash are the only kinds whose free text is a SUBJECT rather
// than the id itself (a stash's subject is the only thing that makes its
// row recognisable once stash@{N} is gone from the link); every other kind,
// including a caller-chosen fallback kind for a shape the table has no row
// for, prints "<kind>: <id>".
func linkDesc(kind, id, subject string) string {
	switch kind {
	case "commit":
		return "commit: " + id + " " + truncateDesc(subject)
	case "stash":
		return "stash: " + truncateDesc(subject)
	default:
		return kind + ": " + truncateDesc(id)
	}
}

// linkRecordFields decides what to pass linkDesc for l, the link a producer
// (`gg link`, `gg compare`) is about to record. Priority:
//
//  1. A copy-source HINT (bookmark/shelf/stash) names the surface the user
//     copied FROM, and wins over the link's own target — spec §4.3's own
//     bookmark/shelf example links both address a commit target, and their
//     Desc is still "bookmark: …" / "shelf: …", not "commit: …".
//  2. Otherwise the link's own target: preview, branch (ref) or commit (rev).
//  3. Otherwise a bare path, if one is set: "file: <path>".
//  4. Otherwise the shape has NO row in spec §4.3's table — a --pair
//     change-set link, a --cached link, or the bare working tree with no
//     path and no target flags. Rather than invent a false row (or leave
//     Desc blank), these fall back to kind "link" with id = the link's own
//     text, which linkDesc's default arm renders as "link: gg://…".
//
// Every lookup here is BEST-EFFORT: a bookmark/shelf that no longer exists,
// or a commit git can no longer show, falls back to the raw id rather than
// making the record — or the copy it describes — fail.
func linkRecordFields(ctx context.Context, svc *domain.Service, l model.Link) (kind, id, subject string) {
	switch l.Hint.Kind {
	case "bookmark":
		label := l.Hint.ID
		if b, err := svc.BookmarkGet(ctx, l.Hint.ID); err == nil && b.Label != "" {
			label = b.Label
		}
		return "bookmark", label, ""
	case "shelf":
		label := l.Hint.ID
		if e, err := svc.ShelfFind(ctx, l.Hint.ID); err == nil && e.Label != "" {
			label = e.Label
		}
		return "shelf", label, ""
	case "stash":
		// No producer wired in this task creates a stash-hinted link (`gg
		// link` has no --stash flag): the grammar and linkhist.Entry both
		// carry the shape, but nothing populates it yet. The hint's own id
		// (a stash index, e.g. "0") is the best available fallback until a
		// producer resolves it to the stash's actual subject.
		return "stash", "", l.Hint.ID
	}
	switch {
	case l.Target.Preview != nil:
		return "preview", l.Target.Preview.Target + "..." + l.Target.Preview.Source, ""
	case l.Target.Ref != "":
		return "branch", l.Target.Ref, ""
	case l.Target.Commit != "":
		short := l.Target.Commit
		if len(short) > 7 {
			short = short[:7]
		}
		subj := ""
		if line, found, err := svc.CommitLookup(ctx, l.Target.Commit); err == nil && found {
			subj = line.Subject
		}
		return "commit", short, subj
	case l.Path != "":
		return "file", l.Path, ""
	default:
		// A --pair change-set (no single name to show), --cached, or the
		// bare working tree: none has a row in spec §4.3's table.
		return "link", l.String(), ""
	}
}

// splitLinkPair splits a --pair argument on the grammar's own two-dot form.
// Three dots are git's OTHER range vocabulary — merge-base(a, b)..b — and gg
// spells that a merge preview, so `a...b` is redirected rather than silently
// read as a two-dot pair with a stray dot in a refname.
func splitLinkPair(spec string) (a, b string, err error) {
	if strings.Contains(spec, "...") {
		return "", "", fmt.Errorf("%w: --pair takes <a>..<b>; use --preview for a merge preview (<target>...<source>)", model.ErrLink)
	}
	i := strings.Index(spec, "..")
	if i < 0 {
		return "", "", fmt.Errorf("%w: --pair takes <a>..<b>, got %q", model.ErrLink, spec)
	}
	a, b = spec[:i], spec[i+2:]
	if a == "" || b == "" {
		return "", "", fmt.Errorf("%w: --pair needs both halves, got %q", model.ErrLink, spec)
	}
	return a, b, nil
}

// resolveLinkPairHalf resolves one half of a --pair to a FULL sha. Full, never
// `%h`: a short sha honours core.abbrev (legal down to 4) while the grammar
// requires 7..64 hex, so an abbreviating resolver would turn a legal repo
// config into a hard failure — this feature's predecessor shipped exactly that
// bug.
func resolveLinkPairHalf(ctx context.Context, svc *domain.Service, rev string) (string, error) {
	full, found, err := svc.ResolveRev(ctx, rev)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return strings.TrimSpace(full), nil
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
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Side     string `json:"side,omitempty"`
	Line     int    `json:"line,omitempty"`
	Hunk     int    `json:"hunk,omitempty"`
	// Ref is the branch or tag NAME when the link named a tip (@ref:<name>);
	// Commit carries the tip as it resolved HERE. PairA/PairB are the
	// change-set's ends when it named one (@<a>..<b>), each a full sha.
	//
	// Without these, a ref link and a pair link resolved to identical JSON —
	// both just `state: commit` plus B — so the one field that distinguishes
	// the two shapes was the one the caller could not see. The MCP tool
	// gg_link_resolve reports the same set; two frontends describing one
	// resolution differently is this feature family's oldest bug.
	Ref      string `json:"ref,omitempty"`
	PairA    string `json:"pair_a,omitempty"`
	PairB    string `json:"pair_b,omitempty"`
	HintKind string `json:"hint_kind,omitempty"`
	HintID   string `json:"hint_id,omitempty"`
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
		if res.Preview != nil {
			w.Source, w.Target = res.Preview.Source, res.Preview.Target
		}
		w.Ref = res.Ref
		if p := res.Pair; p != nil {
			w.PairA, w.PairB = p.A, p.B
		}
		w.HintKind, w.HintID = res.Hint.Kind, res.Hint.ID
		if err := json.NewEncoder(stdout).Encode(w); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintln(stdout, res.Checkout)
	line := res.Addr.State.String()
	switch {
	case res.Ref != "":
		// The NAME is the point of a ref link (ruling R2) — printing only
		// the sha it resolved to today would drop what travels.
		line = "ref " + res.Ref
	case res.Pair != nil:
		line = "pair " + res.Pair.A + ".." + res.Pair.B
	}
	if res.Preview != nil {
		// A preview's state word alone ("committed") would say nothing about
		// WHICH commit or why: name the pair, then the tip it resolved to.
		line = "preview " + res.Preview.Target + "..." + res.Preview.Source
	}
	if res.Addr.Commit != "" && res.Pair == nil {
		// A pair already printed both its ends; appending B again would read
		// as a third commit.
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

// linkShapes is what one verb accepts out of the shapes Task 2 taught
// domain.ResolveLink to hand back: a branch/tag TIP (@ref:<name>, a single
// commit — a POINT, the whole tree there) and a CHANGE-SET (@<a>..<b>,
// BOUNDED — only what it changed). A verb names its own allowance so the
// refusal is decided in ONE place (ruling R4) rather than six scattered
// guards that can drift apart.
type linkShapes struct {
	Ref  bool // a tip is a single commit, so most verbs take it
	Pair bool // BOUNDED; a verb needing one commit must refuse it
}

// resolveLinkArg resolves a link positional for a consumer verb AND applies
// ruling R4's shape gate. There is exactly one such function, and it takes the
// allowance as an argument, so a verb cannot reach a resolver that skips the
// gate: the shorter, ungated three-argument form this replaced no longer
// exists, and a call written from muscle memory —
// `resolveLinkArg(ctx, svc, arg)` — now fails to COMPILE rather than silently
// accepting a shape the verb cannot honour. That is the point. A comment
// saying "do not call the other one" is a convention; a missing function is
// an invariant, and this rule protects an output nobody would look at twice
// (`gg diff` on a change-set printed the newer commit's own change at exit 0
// until the range fix landed beside this gate).
//
// A pair link is BOUNDED: it names what changed between two commits, not one
// place in the tree. A verb that needs a single commit to anchor on — a note,
// `gg show` — must refuse it rather than silently widen it to the whole tree
// at the pair's newer half, which is all Resolved.Commit/Addr.Commit carry.
// verb names the caller in the refusal's own prose, so the message reads as
// that verb's limit rather than the resolver's.
func resolveLinkArg(ctx context.Context, svc *domain.Service, s string, allow linkShapes, verb string) (domain.Resolved, error) {
	l, err := model.ParseLink(s)
	if err != nil {
		return domain.Resolved{}, err
	}
	res, err := domain.ResolveLink(ctx, l, linkResolveOpts(RepoStatePath, svc))
	if err != nil {
		return domain.Resolved{}, err
	}
	if res.Pair != nil && !allow.Pair {
		return domain.Resolved{}, fmt.Errorf("%w: a change-set link names what changed between two commits, not one commit to %s; hand it to `gg compare`", model.ErrLink, verb)
	}
	if res.Ref != "" && !allow.Ref {
		// Unreachable today — every verb in R4's table accepts a tip — but a
		// refusal that fires must still be TRUE. A tip resolves to exactly one
		// commit here (domain.Resolved.Ref's doc: "Commit and Addr.Commit
		// carry the tip as it resolved HERE"), so the objection can only be to
		// the moving NAME, never to the count.
		return domain.Resolved{}, fmt.Errorf("%w: a branch or tag tip link names a moving ref, and %s needs a pinned commit; use the sha", model.ErrLink, verb)
	}
	return res, nil
}

// linkResolveOpts wires the resolver to this process (linknav.Opts): the MRU
// registry, the cwd's service, and the steer-presence probe.
func linkResolveOpts(statePath string, svc *domain.Service) domain.ResolveOpts {
	return linknav.Opts(statePath, svc)
}
