package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// errUnknownRev marks a token git could not resolve to a commit — bad input,
// so exit 2 (usage). A resolve that FAILED instead (the context was
// cancelled) is wrapped plainly and exits 1: a broken repo is not a usage
// error. resolveCompareSpec makes that split with errors.Is.
var errUnknownRev = errors.New("unknown revision")

// parseEndpoint maps a CLI token to a comparison endpoint: "@worktree" (the
// working tree), "@staged"/"@index" (the index), or any other token as a
// commit-ish (HEAD, a branch name, abc123, HEAD~2, …) resolved to a full
// sha through resolve before it reaches model.CommitEndpoint.
//
// The commit-ish MUST be resolved to a sha before it reaches CommitEndpoint:
// Endpoint.CacheTag() returns Hash verbatim and is the session diff-cache
// key, so a git rev-spec there would key the cache on a name that moves —
// a commit↔commit compare re-opened after the name advanced could then be
// served the PREVIOUS diff. The caller passes a resolver bound to its own
// domain.Service (this function has no service access of its own).
//
// The resolver MUST yield a FULL sha (domain.ResolveRev / `git rev-parse`),
// never a `%h` short one: %h honours core.abbrev, git allows it down to 4,
// and model.CommitEndpoint requires 7..64 — so a short-sha resolver turns a
// legal repo config into a hard `gg compare` failure.
func parseEndpoint(s string, resolve func(rev string) (hash string, ok bool, err error)) (model.Endpoint, error) {
	switch s {
	case "@worktree":
		return model.WorkTreeEndpoint(), nil
	case "@staged", "@index":
		return model.IndexEndpoint(), nil
	default:
		hash, ok, err := resolve(s)
		if err != nil {
			return model.Endpoint{}, fmt.Errorf("resolving %q: %w", s, err)
		}
		if !ok {
			return model.Endpoint{}, fmt.Errorf("%w: %s", errUnknownRev, s)
		}
		return model.CommitEndpoint(hash)
	}
}

// compareUsage is printed for every usage error of `gg compare`.
const compareUsage = "usage: gg compare [--patch] <left> [<right>]   " +
	"(endpoints: a gg:// link, a commit, @staged, @worktree, bookmark:<id>, shelf:<id>; right defaults to @worktree)"

// cmdCompare prints the changed-file list (or, with --patch, unified diffs)
// between two endpoints:
//
//	gg compare [--patch] <left> [<right>]
//
// where each endpoint is a gg:// link, a commit-ish, @staged, @worktree, or a
// stored commit entry: bookmark:<id> / shelf:<id> (hybrid — the live sha while
// it exists, a shelved entry's frozen tar after a gc; the fallback is noted on
// stderr). <right> defaults to @worktree. List output is one "<status>\t<path>"
// line per changed file.
//
// THERE IS NO SUCH THING AS AN INVALID PAIR any more. This verb used to screen
// its two endpoints through a validComparePair predicate and refuse a
// "reversed" order (`gg compare @worktree main`) or a frozen shelf entry
// against the live tree. Both refusals were git's own argv limitations showing
// through the CLI, not statements about what a user may ask: domain.CompareSets
// is total over the 2×2 of bounded/unbounded sides, so the pairing rules have
// left this frontend for good. `gg compare shelf:<id> @worktree` now answers
// the real question it used to refuse — "is my shelved work already in my
// working tree?".
func cmdCompare(statePath string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print unified diffs instead of the changed-file list")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	args = fs.Args()
	if len(args) == 0 {
		fmt.Fprintln(stderr, compareUsage)
		return 2
	}
	left, code := resolveCompareSpec(statePath, svc, args[0], stderr)
	if code != 0 {
		return code
	}
	rightTok := "@worktree"
	if len(args) > 1 {
		rightTok = args[1]
	}
	right, code := resolveCompareSpec(statePath, svc, rightTok, stderr)
	if code != 0 {
		return code
	}
	if *patch {
		// ComparePatch takes ENDPOINTS, not the file sets resolved above, so a
		// key set it cannot re-derive from an endpoint is simply LOST: it
		// would describe a different comparison from the one the default
		// listing shows, with nothing on screen to say so. The condition is
		// asked PER SIDE (sideLosesItsKeySet) — asking it per pair is what
		// left `pair × shelf` answering wrongly.
		//
		// The reachable shape is not an exotic one. Any link with a /<path>
		// is bounded, and that is the spelling every "copy gg link" button in
		// the product emits.
		//
		// TODO(plan 3): what is DEFERRED is rendering a PROJECTED patch for
		// the lanes that lose the set — a set-taking ComparePatch sibling,
		// which is a new question (which hunks of a file a projection even
		// contains), not a signature change. Deferred alongside it: inverting
		// a reversed LIVE pair, which needs a Reverse flag on model.DiffSpec
		// (it has no `-R`) and three new argv forms; that one still surfaces
		// below as ErrComparePatchPair.
		if patchLosesTheKeySet(left, right) {
			fmt.Fprintf(stderr, "compare: --patch renders whole endpoints, and %s a file set "+
				"(a link with a /<path>, or an <a>..<b> change-set); "+
				"drop --patch for the changed-file list of exactly those files\n",
				boundedSideName(left, right))
			return 2
		}
		diff, err := svc.ComparePatch(context.Background(), left.Endpoint(), right.Endpoint())
		if err != nil {
			// The one gap gets gg's own words. domain's refusal names a Go
			// function and two raw enum ordinals ("livePairSpec: unsupported
			// endpoint pair 1 → 3"), and a user's terminal is the wrong place
			// for either — the message it REPLACED ("order endpoints
			// oldest→newest…") at least told the user what to type.
			if errors.Is(err, domain.ErrComparePatchPair) {
				fmt.Fprintf(stderr, "compare: --patch cannot render %s → %s yet; "+
					"drop --patch for the changed-file list, or order the endpoints oldest→newest "+
					"(a commit, then @staged, then @worktree)\n",
					left.Endpoint().Display(), right.Endpoint().Display())
				return 2
			}
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprint(stdout, diff)
		return 0
	}
	files, err := svc.CompareSets(context.Background(), left, right)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	printCompareFiles(stdout, files)
	return 0
}

// resolveCompareSpec turns one CLI token into the FILE SET it names.
//
// A gg:// link is the primary spelling and comes first — not a mode: either
// side may be a link, and a link mixes freely with the old vocabulary. A link
// is the one token that can name a BOUNDED side (a `@<a>..<b>` change-set, or
// any link with a /<path>), which is why this returns a domain.FileSet and not
// a model.Endpoint: an Endpoint alone cannot say "these files", only "this
// text".
//
// bookmark:<id> and shelf:<id> address a stored commit entry and resolve
// hybrid (live sha while it exists, frozen tar for a gc'd shelved commit —
// noted on stderr so stdout stays parseable); anything else is the existing
// vocabulary (@worktree/@staged/commit-ish — a commit-ish is resolved to a
// FULL sha via svc.ResolveRev before parseEndpoint builds the Endpoint). The
// int is an exit code: 0 = resolved, 1 = failure (gone bookmark, a failed
// resolve), 2 = usage (a malformed link, an unknown id / not a commit entry /
// unresolvable commit-ish).
//
// KNOWN GAP: ResolveRev follows the domain's "missing is not an error"
// convention (queryQuiet discards the git error unless the context was
// cancelled), so a repo that is locked or corrupt is indistinguishable here
// from a typo'd rev and reports as "unknown revision" with exit 2.
// Separating the two needs a domain-level change to ResolveRev's contract.
func resolveCompareSpec(statePath string, svc *domain.Service, tok string, stderr io.Writer) (domain.FileSet, int) {
	ctx := context.Background()
	switch {
	case isLinkArg(tok):
		return compareLinkSet(ctx, statePath, svc, tok, stderr)
	case strings.HasPrefix(tok, "bookmark:"):
		id := strings.TrimPrefix(tok, "bookmark:")
		b, err := svc.BookmarkGet(ctx, id)
		if err != nil {
			fmt.Fprintf(stderr, "compare: bookmark %q: %v\n", id, err)
			return domain.FileSet{}, 2
		}
		if !b.IsCommit() {
			fmt.Fprintf(stderr, "compare: bookmark %q is a file bookmark, not a commit\n", id)
			return domain.FileSet{}, 2
		}
		ep, err := svc.ResolveCommitEntryEndpoint(ctx, b.Commit, "")
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			return domain.FileSet{}, 1
		}
		return evalCompareEndpoint(ctx, svc, ep, stderr)
	case strings.HasPrefix(tok, "shelf:"):
		id := strings.TrimPrefix(tok, "shelf:")
		e, err := svc.ShelfFind(ctx, id)
		if err != nil {
			fmt.Fprintf(stderr, "compare: shelf %q: %v\n", id, err)
			return domain.FileSet{}, 2
		}
		if !e.IsCommit() {
			fmt.Fprintf(stderr, "compare: shelf entry %q is a file entry, not a commit\n", id)
			return domain.FileSet{}, 2
		}
		ep, err := svc.ResolveCommitEntryEndpoint(ctx, e.Origin.Commit, e.ID)
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			return domain.FileSet{}, 1
		}
		if ep.Kind() == model.EndpointShelf {
			sha := e.Origin.Commit
			if len(sha) > 7 {
				sha = sha[:7]
			}
			fmt.Fprintf(stderr, "# frozen compare: commit %s no longer exists\n", sha)
		}
		return evalCompareEndpoint(ctx, svc, ep, stderr)
	default:
		// ResolveRev, not CommitLookup: CommitLookup's %h honours
		// core.abbrev (legal down to 4) and CommitEndpoint requires 7..64,
		// so a short-sha resolver made a legal repo config a hard failure.
		ep, err := parseEndpoint(tok, func(rev string) (string, bool, error) {
			return svc.ResolveRev(ctx, rev)
		})
		if err != nil {
			fmt.Fprintln(stderr, "compare:", err)
			if errors.Is(err, errUnknownRev) {
				return domain.FileSet{}, 2 // bad input
			}
			return domain.FileSet{}, 1 // the resolve itself failed
		}
		return evalCompareEndpoint(ctx, svc, ep, stderr)
	}
}

// evalCompareEndpoint turns one resolved endpoint into its file set. Every
// non-link arm ends here, so a shelf entry's bounded member list and a
// commit's whole tree reach CompareSets through the same door.
func evalCompareEndpoint(ctx context.Context, svc *domain.Service, ep model.Endpoint, stderr io.Writer) (domain.FileSet, int) {
	fs, err := svc.EvalEndpoint(ctx, ep)
	if err != nil {
		fmt.Fprintln(stderr, "compare:", err)
		return domain.FileSet{}, 1
	}
	return fs, 0
}

// compareLinkSet evaluates a gg:// link argument to its file set.
//
// THE LINK IS LOCATED BEFORE IT IS EVALUATED, and that order is a correctness
// requirement rather than a formality. A PARSED local-form link
// (gg:///abs/checkout/dir/f.go) carries the checkout AND the file path
// undivided in Repo.Abs with Link.Path == "" — the grammar puts no delimiter
// between them, and only this machine's repository registry can split them.
// Hand an unresolved local link to EvalLink and a FILE link silently evaluates
// as a WHOLE-TREE link: a wrong answer with no error, which is the one class
// this feature refuses to ship. domain.LocateLink performs the split (and
// refuses a path that escapes the checkout); the located path is then the
// link's Path, while the TARGET stays exactly as parsed — a `@<a>..<b>`
// change-set must not be flattened into an address, which is why this goes
// through LocateLink and not ResolveLink.
func compareLinkSet(ctx context.Context, statePath string, svc *domain.Service, tok string, stderr io.Writer) (domain.FileSet, int) {
	l, err := model.ParseLink(tok)
	if err != nil {
		fmt.Fprintln(stderr, "compare:", err)
		return domain.FileSet{}, 2
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "compare:", err)
		return domain.FileSet{}, 1
	}
	checkout, rel, err := domain.LocateLink(ctx, l, linkResolveOpts(statePath, svc))
	if err != nil {
		return domain.FileSet{}, linkExit("compare", err, stderr)
	}
	// Cross-repository compare is deferred (spec §9). Both sides would evaluate
	// to file sets happily, but every read goes through ONE domain.Service under
	// ONE repogate reservation, so two repositories need two reservations taken
	// in a fixed global order to avoid deadlock. Refuse explicitly rather than
	// silently compare against the wrong checkout.
	if !domain.SameCheckout(checkout, top) {
		fmt.Fprintf(stderr, "compare: %s names a different repository (%s); cross-repository compare is not supported yet\n", tok, checkout)
		return domain.FileSet{}, 2
	}
	l.Path = rel
	fs, err := svc.EvalLink(ctx, l)
	if err != nil {
		fmt.Fprintln(stderr, "compare:", err)
		if errors.Is(err, model.ErrLink) {
			return domain.FileSet{}, 2
		}
		return domain.FileSet{}, 1
	}
	return fs, 0
}

// sideLosesItsKeySet reports whether ONE side's key set survives the trip
// through ComparePatch, which takes endpoints and re-derives what it needs
// from them. The question is per SIDE, not per pair: a set survives exactly
// when EvalEndpoint(fs.Endpoint()) would reproduce it.
//
//   - UNBOUNDED — nothing to lose; its endpoint IS the whole tree.
//   - SHELF endpoint, not narrowed — ComparePatch's shelf lane re-derives both
//     sides with EvalEndpoint and renders per member, so this comes back
//     identical. That is the shipped `gg compare --patch shelf:<gc'd id>`
//     answer, and it must keep working.
//   - NARROWED — a projection (a link's /<path>). narrowTo carries the
//     endpoint over untouched, so re-deriving widens it straight back to the
//     endpoint's own set. This is why a narrowed SHELF set still loses, and
//     why FileSet.Narrowed has to exist: bounded-ness and kind cannot see it.
//   - Any other BOUNDED set — above all a PAIR, whose endpoint is commit *b*
//     and not the pair (EvalEndpoint's Pair arm), so re-deriving yields b's
//     whole tree. livePairSpec's lane does not consult the sets at all, and
//     the shelf lane re-derives from the wrong thing; either way the key set
//     is gone.
//
// Asking per pair is what left `pair × shelf` open: "a shelf is on one side,
// so ComparePatch re-derives both sets" is true of the SHELF side only. The
// change-set's members vanished from the patch while a file it never named
// was rendered — the same silent wrong answer this guard exists to stop.
func sideLosesItsKeySet(fs domain.FileSet) bool {
	return fs.Narrowed() || (fs.Bounded() && fs.Endpoint().Kind() != model.EndpointShelf)
}

// patchLosesTheKeySet is sideLosesItsKeySet over the pair: --patch may run
// only when NEITHER side would be quietly widened.
func patchLosesTheKeySet(left, right domain.FileSet) bool {
	return sideLosesItsKeySet(left) || sideLosesItsKeySet(right)
}

// boundedSideName names the side to blame, as a full clause — the verb travels
// with the subject ("both sides name", not "both sides" + " names"), because
// splitting them shipped "and both sides name names a file set". Built from
// the SAME predicate the refusal is built from, so the message cannot name a
// side the guard did not fire on.
func boundedSideName(left, right domain.FileSet) string {
	lb, rb := sideLosesItsKeySet(left), sideLosesItsKeySet(right)
	switch {
	case lb && rb:
		return "both sides name"
	case lb:
		return "the left side names"
	default:
		return "the right side names"
	}
}
