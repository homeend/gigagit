package domain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

var (
	// ErrLinkAmbiguous means several checkouts on this machine answer to the
	// link and nothing distinguishes them. The resolver never guesses: two
	// worktrees of one repo hold DIFFERENT working-tree files, so a wrong
	// guess would silently point at the wrong content.
	ErrLinkAmbiguous = errors.New("gg link is ambiguous")
	// ErrLinkUnknownRepo means no checkout on this machine answers to the
	// link. The registry is the only source; gg never scans the filesystem.
	ErrLinkUnknownRepo = errors.New("gg link names an unknown repository")
)

// Resolved is a link turned into a place on THIS machine.
type Resolved struct {
	Checkout string            // absolute top level of the chosen checkout
	Addr     model.FileAddress // Worktree = Checkout for the working-tree states
	Line     int
	Side     model.NoteSide
	Hunk     int
	Commit   string // the FULL sha when the link named a commit
	// Preview is the resolved merge-preview scope when the link named a pair
	// (gg://<repo>@<target>...<source>). Addr and Commit then point at the
	// preview's SOURCE TIP — the write target for a preview note (spec §1.1) —
	// and every consumer that would otherwise read a --preview argument reads
	// this instead. nil for every other link.
	Preview *PreviewNoteSet
	// Ref is the branch or tag NAME when the link named a tip
	// (@ref:<name>). Commit and Addr.Commit carry the tip as it resolved
	// HERE, so a caller that wants an address has one — but the NAME is
	// what travels, and every consumer re-resolves it (ruling R2).
	Ref string
	// Pair is the change-set when the link named one (@<a>..<b>), each half
	// resolved to a FULL sha on the chosen checkout. Commit and Addr.Commit
	// carry B: a change-set's newer end is the only single commit it has,
	// and a consumer that needs one (a note, a `gg show`) is refused by
	// ruling R4 rather than silently handed it.
	Pair *model.LinkPair
	// Hint is the UI surface the link was copied from (spec §3.3): it never
	// changes WHERE this resolves, only which surface a consumer reveals once
	// it lands. Copied blind from the link in finishLink's common prologue —
	// a consumer with an address looks the entry up itself; ResolveLink only
	// checks presence when there is no address at all, because then the hint
	// is the link's only content (see finishLink).
	Hint model.LinkHint
}

// ResolveOpts carries everything the resolver may not reach for itself, so a
// parallel test can supply all of it.
type ResolveOpts struct {
	// RegistryPath is the repo-switcher registry. "" (or a missing file)
	// simply means "no registry candidates" — never an error.
	RegistryPath string
	// Cwd is the service for the directory the caller ran in, or nil. When it
	// matches the link's identity it wins outright.
	Cwd *Service
	// LiveFn reports whether a gg session is live for a checkout. nil = none
	// is. The CLI supplies the real steer-presence probe; domain must not
	// import internal/steer.
	LiveFn func(commonDir, checkout string) bool
	// OpenFn opens a service for another checkout. nil = domain.Open.
	OpenFn func(dir string) *Service
}

// linkCandidate is one checkout that answers to the link, plus the file path
// within it (a local link's path is only known after the split).
type linkCandidate struct {
	checkout string
	relPath  string
	isCwd    bool
}

// ResolveLink turns a link into a checkout on this machine.
//
// Candidate order (spec §2): the cwd's repo when its identity matches, then
// every registry entry (MRU order) whose identity matches. Several matches
// are narrowed by containment of the target commit, then by a live gg
// session; a WORKING-TREE link that still has more than one candidate is
// refused (ErrLinkAmbiguous) rather than guessed, because two checkouts hold
// different working files. A COMMIT link takes the most recently opened
// container — every container holds byte-identical content, so there is
// nothing to get wrong.
func ResolveLink(ctx context.Context, l model.Link, opts ResolveOpts) (Resolved, error) {
	// A cancelled resolve must say so. Every candidate probe below swallows
	// its own error (a dead entry is simply not a candidate), so without this
	// a cancelled context would surface as "not in this machine's gg history"
	// — a factual claim gg has not actually established.
	if err := ctx.Err(); err != nil {
		return Resolved{}, err
	}
	if opts.OpenFn == nil {
		opts.OpenFn = Open
	}
	// StateCommitted is FileState's ZERO value, so a Link built programmatically
	// (not via ParseLink, which never leaves Commit empty for this state) could
	// slip through with no sha. Refuse it here rather than reaching finishLink's
	// ResolveRev with an empty ref. A ref or pair link is StateCommitted with an
	// EMPTY Commit BY DESIGN (the tip/halves are per-machine resolutions), so
	// both are exempted from this guard.
	if l.Target.State == model.StateCommitted && l.Target.Commit == "" &&
		l.Target.Preview == nil && l.Target.Ref == "" && l.Target.Pair == nil {
		return Resolved{}, fmt.Errorf("%w: gg link names a commit without a sha", model.ErrLink)
	}
	c, err := locateLink(ctx, l, opts)
	if err != nil {
		return Resolved{}, err
	}
	return finishLink(ctx, l, c, opts)
}

// LocateLink answers the FIRST half of ResolveLink's question and only that
// half: which checkout on this machine the link names, and the link's path
// inside it.
//
// It exists because the second half — turning the target into an ADDRESS —
// cannot express every link the way a consumer of the RAW target needs it. A
// `@ref:<name>` tip is a MOVING point and a `@<a>..<b>` change-set is BOUNDED
// (model.FileAddress has no field for either shape: ResolveLink's Resolved
// carries the tip/halves as it saw them HERE, which is an address of sorts,
// but collapses the moving name or the bounded set into a single point). The
// SPLIT, though, is exactly as necessary for those links as for any other: a
// parsed LOCAL-form link carries the checkout and the file path undivided in
// Repo.Abs with Link.Path empty (see model.LinkRepo), and only this machine's
// registry can divide them.
//
// So a consumer that evaluates a link's own target itself — domain.EvalLink's
// callers, which need the NAME or the BOUNDED pair, not a frozen point —
// locates with this, takes relPath as the link's Path, and keeps the target it
// parsed. A consumer that wants an address keeps calling ResolveLink.
//
// The returned path is repo-relative, cleaned, and never escapes the checkout.
func LocateLink(ctx context.Context, l model.Link, opts ResolveOpts) (checkout, relPath string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if opts.OpenFn == nil {
		opts.OpenFn = Open
	}
	c, err := locateLink(ctx, l, opts)
	if err != nil {
		return "", "", err
	}
	rel, err := cleanLinkRelPath(c.relPath)
	if err != nil {
		return "", "", err
	}
	return c.checkout, rel, nil
}

// locateLink is the candidate pipeline both entry points share: identity
// match, then the narrowing filters, then the ambiguity refusal. It performs no
// address resolution, so a ref or pair link locates here even though
// ResolveLink will not finish one.
//
// opts.OpenFn is already defaulted by the caller.
func locateLink(ctx context.Context, l model.Link, opts ResolveOpts) (linkCandidate, error) {
	cands := linkCandidates(ctx, l, opts)
	if len(cands) == 0 {
		return linkCandidate{}, fmt.Errorf("%w: %s is not in this machine's gg history; open it once in gg", ErrLinkUnknownRepo, linkRepoLabel(l))
	}
	// A preview names two BRANCHES, so a checkout that lacks either cannot show
	// it — drop those before anything else, including the cwd. (A commit link's
	// containment filter below runs only when more than one candidate is left;
	// this one runs ALWAYS, because "the cwd has the repo but not the branch" is
	// the common case, not the tie-break case.) The cwd still sorts first, so it
	// wins any tie the filter leaves.
	if p := l.Target.Preview; p != nil {
		kept := previewCandidates(ctx, cands, p, opts)
		if len(kept) == 0 {
			return linkCandidate{}, fmt.Errorf("%w: no checkout of %s holds both %s and %s", ErrLinkUnknownRepo, linkRepoLabel(l), p.Target, p.Source)
		}
		cands = kept
	}
	// A ref or pair link names REFS, so a checkout that cannot resolve them
	// cannot show it. Like previewCandidates this runs ALWAYS, not only on a
	// tie: a ref link is StateCommitted with an EMPTY Commit, so it skips the
	// containment filter below (Commit != "" is load-bearing there) and would
	// otherwise take the MRU checkout whether or not it holds the branch.
	if names := linkTargetRefs(l); len(names) > 0 {
		kept := resolvingAll(ctx, cands, names, opts)
		if len(kept) == 0 {
			return linkCandidate{}, fmt.Errorf("%w: no checkout of %s resolves %s", ErrLinkUnknownRepo, linkRepoLabel(l), strings.Join(names, " and "))
		}
		cands = kept
	}
	// The cwd is never ambiguous: the caller ran the command THERE, which is
	// the strongest statement of intent available. A commit link still goes
	// through containment below — the cwd may not hold that commit — and the
	// cwd sorts first, so it wins any tie the filters leave.
	if cands[0].isCwd && l.Target.State != model.StateCommitted {
		return cands[0], nil
	}
	// Commit != "" is load-bearing since LocateLink arrived: a ref or pair link
	// is StateCommitted with an EMPTY Commit, and probing containment of ""
	// would spend one git invocation per candidate to learn nothing.
	if len(cands) > 1 && l.Target.State == model.StateCommitted && l.Target.Preview == nil && l.Target.Commit != "" {
		if kept := containing(ctx, cands, l.Target.Commit, opts); len(kept) > 0 {
			cands = kept
		}
	}
	if len(cands) > 1 && opts.LiveFn != nil {
		var live []linkCandidate
		for _, c := range cands {
			// OpenFn is defaulted once at this function's entry and never nil.
			cd, _ := opts.OpenFn(c.checkout).GitCommonDir(ctx)
			if opts.LiveFn(cd, c.checkout) {
				live = append(live, c)
			}
		}
		// Narrow to the live subset whenever ANY candidate is live — even
		// when more than one is (e.g. two live gg sessions on shared
		// clones); MRU / commit-containment order then breaks any remaining
		// tie. Only an EMPTY live set leaves the candidates untouched.
		if len(live) > 0 {
			cands = live
		}
	}
	if len(cands) > 1 && l.Target.State != model.StateCommitted {
		var paths []string
		for _, c := range cands {
			paths = append(paths, c.checkout)
		}
		return linkCandidate{}, fmt.Errorf("%w: %s matches %s; run the command inside the one you mean", ErrLinkAmbiguous, linkRepoLabel(l), strings.Join(paths, ", "))
	}
	return cands[0], nil
}

// linkCandidates lists every checkout whose identity matches the link, cwd
// first, then the registry in MRU order, deduplicated by path.
func linkCandidates(ctx context.Context, l model.Link, opts ResolveOpts) []linkCandidate {
	var out []linkCandidate
	seen := map[string]bool{}
	add := func(c linkCandidate) {
		key := linkPathKey(c.checkout)
		if c.checkout == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, c)
	}
	entries := repos.Load(opts.RegistryPath)
	if l.IsLocal() {
		// Longest path-boundary prefix wins, contested across the cwd AND the
		// registry TOGETHER: a worktree nested inside another checkout —
		// including the cwd's OWN checkout — must not be shadowed by its
		// parent. isCwd is set only when the cwd actually WINS the contest,
		// so ResolveLink's cwd short-circuit below never picks a checkout
		// that does not actually contain the link's path.
		best, bestRel, bestLen, bestIsCwd := "", "", -1, false
		consider := func(checkout string, isCwd bool) {
			if rel, ok := linkSplit(l.Repo.Abs, checkout); ok && len(checkout) > bestLen {
				best, bestRel, bestLen, bestIsCwd = checkout, rel, len(checkout), isCwd
			}
		}
		if opts.Cwd != nil {
			if top, err := opts.Cwd.TopLevel(ctx); err == nil && top != "" {
				consider(top, true)
			}
		}
		for _, e := range entries {
			consider(e.Path, false)
		}
		if best != "" {
			add(linkCandidate{checkout: best, relPath: bestRel, isCwd: bestIsCwd})
			return out
		}
		// The link's old location may still exist as a REAL, unregistered
		// checkout — an ancestor of it holds a .git. Guessing by base name in
		// that case could silently point at a DIFFERENT repository that
		// merely shares a directory name with the intended one, so refuse
		// (ruling P5a) rather than fall back.
		if !ancestorHasGit(l.Repo.Abs) {
			// Moved-checkout fallback: the recorded location is gone, so
			// match by directory NAME against the link's own path segments
			// (last occurrence first, so a repo called "src" inside
			// ".../src/src" resolves deepest).
			for _, e := range entries {
				rel, prefix, ok := linkMovedSplit(l.Repo.Abs, filepath.Base(e.Path))
				if !ok {
					continue
				}
				// The guess is only safe while the OLD checkout is really
				// gone. ancestorHasGit above answers that for the link's
				// deepest surviving directory; this answers it for the
				// candidate's own matched prefix, which for a file NESTED
				// below the checkout top is a different directory entirely
				// (".../test-1" vs ".../test-1/sub"). Ruling P5a: never guess
				// at a different repository that merely shares a name.
				if hasGitEntry(filepath.FromSlash(prefix)) {
					continue
				}
				add(linkCandidate{checkout: e.Path, relPath: rel})
			}
		}
		return out
	}
	if opts.Cwd != nil {
		if top, err := opts.Cwd.TopLevel(ctx); err == nil && top != "" {
			if name, err := opts.Cwd.RepoName(ctx); err == nil && linkNameEq(name, l.Repo.Name) {
				add(linkCandidate{checkout: top, relPath: l.Path, isCwd: true})
				// ResolveLink short-circuits on exactly this pair (a cwd
				// candidate + a non-commit target), so walking the registry
				// afterwards is dead work — and not cheap dead work: every
				// entry with no stored remote costs a repository open and two
				// git invocations under that repo's own Read reservation.
				if l.Target.State != model.StateCommitted {
					return out
				}
			}
		}
	}
	for _, e := range entries {
		name := e.Remote
		if name == "" {
			// Lazy backfill for an entry an older gg wrote — or one that has
			// no remote at all, which is memoised as repos.NoRemote ("-") so a
			// remoteless checkout is probed ONCE instead of on every resolve
			// forever. linkNameEq refuses the sentinel, so it can never match
			// a link. SetRemote does not bump LastOpened: resolving is not
			// opening. OpenFn is defaulted once at ResolveLink's entry and
			// never nil.
			n, err := opts.OpenFn(e.Path).RepoName(ctx)
			if err == nil {
				if n == "" {
					n = repos.NoRemote
				}
				name = n
				_ = repos.SetRemote(opts.RegistryPath, e.Path, n)
			}
		}
		if linkNameEq(name, l.Repo.Name) {
			add(linkCandidate{checkout: e.Path, relPath: l.Path})
		}
	}
	return out
}

// ancestorHasGit reports whether the link's old location still lives inside a
// checkout: it walks up from abs to the DEEPEST EXISTING directory and asks
// only that one whether it holds a ".git" entry (file or dir). That
// distinguishes "the old location is genuinely gone" (safe to guess by base
// name) from "something still lives there, just unregistered" (never guess:
// ruling P5a).
//
// Testing only the deepest existing directory — rather than every ancestor up
// to the filesystem root — is what keeps a dotfiles repository in $HOME (or a
// .git at "/") from disabling the moved-checkout fallback for every link
// underneath it: what matters is whether the gone path's own surviving parent
// is a checkout, not whether some distant ancestor is.
func ancestorHasGit(abs string) bool {
	dir := filepath.Clean(abs)
	for {
		if fi, err := os.Stat(dir); err == nil {
			if !fi.IsDir() {
				// abs itself still exists as a FILE: its directory is the
				// deepest existing ancestor.
				dir = filepath.Dir(dir)
				continue
			}
			return hasGitEntry(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// hasGitEntry reports whether dir holds a ".git" entry (a directory in a
// normal checkout, a file in a worktree or a submodule).
func hasGitEntry(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// containing keeps the candidates whose object database holds sha.
func containing(ctx context.Context, cands []linkCandidate, sha string, opts ResolveOpts) []linkCandidate {
	var kept []linkCandidate
	for _, c := range cands {
		// OpenFn is defaulted once at ResolveLink's entry and never nil.
		if _, found, err := opts.OpenFn(c.checkout).ResolveRev(ctx, sha); err == nil && found {
			kept = append(kept, c)
		}
	}
	return kept
}

// previewCandidates keeps the checkouts that can actually SHOW the pair: both
// branch names must resolve there. ResolveRev is the same probe containing()
// uses for a commit link; a candidate whose probe errors is simply not a
// candidate, exactly as there.
func previewCandidates(ctx context.Context, cands []linkCandidate, p *model.LinkPreview, opts ResolveOpts) []linkCandidate {
	var kept []linkCandidate
	for _, c := range cands {
		// OpenFn is defaulted once at ResolveLink's entry and never nil.
		svc := opts.OpenFn(c.checkout)
		if _, found, err := svc.ResolveRev(ctx, p.Source); err != nil || !found {
			continue
		}
		if _, found, err := svc.ResolveRev(ctx, p.Target); err != nil || !found {
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

// linkTargetRefs lists the ref names a link's TARGET needs resolved on the
// checkout that shows it: the tip for @ref:<name>, both halves for @<a>..<b>.
// A preview has its own filter (its halves are branch names by definition);
// every other target names at most a sha, which containment already covers.
func linkTargetRefs(l model.Link) []string {
	if l.Target.Ref != "" {
		return []string{l.Target.Ref}
	}
	if p := l.Target.Pair; p != nil {
		return []string{p.A, p.B}
	}
	return nil
}

// resolvingAll keeps the candidates on which EVERY name resolves. ResolveRev
// is the same probe containing() and previewCandidates() use, and a candidate
// whose probe errors is simply not a candidate, exactly as there.
func resolvingAll(ctx context.Context, cands []linkCandidate, names []string, opts ResolveOpts) []linkCandidate {
	var kept []linkCandidate
	for _, c := range cands {
		svc := opts.OpenFn(c.checkout)
		ok := true
		for _, n := range names {
			if _, found, err := svc.ResolveRev(ctx, n); err != nil || !found {
				ok = false
				break
			}
		}
		if ok {
			kept = append(kept, c)
		}
	}
	return kept
}

// hintOnlyTarget reports whether t names no PINNED content at all — no
// commit, no ref, no pair, no preview. This is the part of "is this link
// address-less" that finishLink's address-less presence check and
// EndpointForLink's address-less-shelf-endpoint check (evallink.go) both
// need identically (fix F6), so it is shared — but it is NOT, by itself,
// either caller's whole answer, because the two ask genuinely different
// questions once a pinned target is ruled out:
//
//   - finishLink additionally requires an EMPTY PATH: its question is "does
//     this link name a PLACE at all" (RepoOnly's own definition — repo +
//     optional path + optional target), which decides where a navigate
//     lands. `gg://<repo>/f.txt?shelf=X` has a real place (the working-tree
//     file f.txt) and is never address-less, no matter the hint.
//   - EndpointForLink additionally requires State to be Unstaged or Staged
//     (never the zero-value StateCommitted a hand-built Link with no sha
//     would otherwise silently pass through — its own switch below refuses
//     that case instead). Its question is "does this link name a PINNED,
//     immutable byte source, or the live, mutable working tree/index" — and
//     that answer does NOT depend on path at all: path-narrowing
//     (EvalLink's narrowTo) is a separate step applied AFTER the endpoint is
//     chosen, so `gg://<repo>/f.txt?shelf=X` (a real path, no pinned
//     target) still substitutes the STABLE shelf snapshot for the
//     unstable live file — this is deliberate, existing behaviour
//     (TestEndpointForLinkShelfHintIsTheOnlyContentSource's `shelved` case)
//     and not something this fix may change.
//
// Before this, EndpointForLink read `t.State == model.StateUnstaged` alone
// — the exact predicate finishLink was corrected away from earlier in this
// same task, for the same reason it was wrong here too: `gg://<repo>@staged
// ?shelf=X` is address-less by RepoOnly's own definition (StateStaged, not
// StateUnstaged) and the old predicate missed it.
func hintOnlyTarget(t model.LinkTarget) bool {
	return t.Commit == "" && t.Preview == nil && t.Ref == "" && t.Pair == nil
}

// finishLink builds the Resolved value for the chosen candidate: the address,
// the worktree pin for the live states, and the FULL sha for a commit link.
func finishLink(ctx context.Context, l model.Link, c linkCandidate, opts ResolveOpts) (Resolved, error) {
	rel, err := cleanLinkRelPath(c.relPath)
	if err != nil {
		return Resolved{}, err
	}
	res := Resolved{
		Checkout: c.checkout,
		Addr:     l.Address(),
		Line:     l.Line,
		Side:     l.Side,
		Hunk:     l.Hunk,
		Hint:     l.Hint,
	}
	if res.Side == "" {
		res.Side = model.NoteSideNew
	}
	res.Addr.Path = rel

	// S11: a hint is checked in two layers. WITH an address, domain copies it
	// blind (the assignment above) — the consumer looks the entry up in its
	// own already-loaded store and reveals or notices; no store lookup runs
	// here. WITHOUT one, the hint is the link's only content, so its
	// presence must be checked HERE: there is nothing else for the link to
	// resolve to if it is gone. hintOnlyTarget (fix F6: shared with
	// EndpointForLink) plus an empty path together answer "does this link
	// name a place at all" — asked of l itself, never of the not-yet-
	// computed res.Commit — this runs before the switch below fills it in.
	if rel == "" && hintOnlyTarget(l.Target) && l.Hint.Kind != "" {
		switch l.Hint.Kind {
		case "bookmark":
			if _, err := opts.OpenFn(c.checkout).BookmarkGet(ctx, l.Hint.ID); err != nil {
				return Resolved{}, fmt.Errorf("%w: %s has no %s %q, and the link names nothing else", model.ErrLink, c.checkout, l.Hint.Kind, l.Hint.ID)
			}
		case "shelf":
			if _, err := opts.OpenFn(c.checkout).ShelfFind(ctx, l.Hint.ID); err != nil {
				return Resolved{}, fmt.Errorf("%w: %s has no %s %q, and the link names nothing else", model.ErrLink, c.checkout, l.Hint.Kind, l.Hint.ID)
			}
		case "preview":
			// A saved preview or pair IS its address — the hint only says
			// where it was copied from — so without one the link names
			// nothing, whether or not this store holds the id.
			return Resolved{}, fmt.Errorf("%w: a preview hint needs the set it names (@<target>...<source> or @<a>..<b>); %q alone names nothing", model.ErrLink, l.Hint.ID)
		case model.ContentHintKind:
			// A content link names a FILE's content; with no path it names
			// nothing. (The remote form is refused by ParseLink already; the
			// local form only learns its path here.)
			return Resolved{}, fmt.Errorf("%w: a content link needs a file path", model.ErrLink)
		default:
			// "stash" (spec §3.4) and any future kind this build cannot
			// check: there is no presence lookup to fall back on, so an
			// address-less link naming one is always a hard error rather
			// than a silent pass-through.
			return Resolved{}, fmt.Errorf("%w: %s has no way to check a %s hint (%q), and the link names nothing else", model.ErrLink, c.checkout, l.Hint.Kind, l.Hint.ID)
		}
	}

	switch l.Target.State {
	case model.StateCommitted:
		if r := l.Target.Ref; r != "" {
			// The NAME is what travels and what every consumer re-resolves
			// (ruling R2); the tip is resolved here only so a caller wanting
			// an ADDRESS has one. ResolveRev peels to ^{commit} and expands to
			// the full sha — the same call the commit arm makes.
			full, found, err := opts.OpenFn(c.checkout).ResolveRev(ctx, r)
			if err != nil {
				return Resolved{}, err
			}
			if !found {
				return Resolved{}, fmt.Errorf("%w: %s does not resolve %s", ErrLinkUnknownRepo, c.checkout, r)
			}
			r2 := strings.TrimSpace(full)
			res.Ref, res.Commit, res.Addr.Commit = r, r2, r2
			return res, nil
		}
		if p := l.Target.Pair; p != nil {
			svc := opts.OpenFn(c.checkout)
			a, aok, aerr := svc.ResolveRev(ctx, p.A)
			b, bok, berr := svc.ResolveRev(ctx, p.B)
			if aerr != nil {
				return Resolved{}, aerr
			}
			if berr != nil {
				return Resolved{}, berr
			}
			if !aok || !bok {
				return Resolved{}, fmt.Errorf("%w: %s does not resolve %s..%s", ErrLinkUnknownRepo, c.checkout, p.A, p.B)
			}
			// Commit carries B — the change-set's newer end and its only
			// single commit. Ruling R4 refuses the verbs that would anchor on
			// it rather than let a bounded set widen into the tree at B.
			res.Pair = &model.LinkPair{A: strings.TrimSpace(a), B: strings.TrimSpace(b)}
			res.Commit, res.Addr.Commit = res.Pair.B, res.Pair.B
			return res, nil
		}
		if p := l.Target.Preview; p != nil {
			// The tip is resolved HERE, on the chosen checkout, so every
			// consumer gets exactly the set `--preview <target>...<source>`
			// would build (ruling 3). PreviewNotes returns a ZERO set with a nil
			// error for a pair that is not previewable (merged, no common base):
			// that is not an error there, but it is one here — the caller asked
			// for this preview by name.
			set, perr := opts.OpenFn(c.checkout).PreviewNotes(ctx, p.Source, p.Target)
			if perr != nil {
				return Resolved{}, perr
			}
			if !set.OK() {
				return Resolved{}, fmt.Errorf("%w: %s does not show %s...%s (merged, or no common base)", ErrLinkUnknownRepo, c.checkout, p.Target, p.Source)
			}
			res.Preview = &set
			res.Commit, res.Addr.Commit = set.Tip, set.Tip
			return res, nil
		}
		// ResolveRev peels to ^{commit} and is also the >=7-hex → full-sha
		// expansion: no second verb exists for that. OpenFn is defaulted once
		// at ResolveLink's entry and never nil.
		full, found, err := opts.OpenFn(c.checkout).ResolveRev(ctx, l.Target.Commit)
		if err != nil {
			return Resolved{}, err
		}
		if !found {
			return Resolved{}, fmt.Errorf("%w: %s does not contain commit %s", ErrLinkUnknownRepo, c.checkout, l.Target.Commit)
		}
		full = strings.TrimSpace(full)
		res.Commit, res.Addr.Commit = full, full
	default:
		res.Addr.Worktree = c.checkout
	}
	return res, nil
}

// cleanLinkRelPath cleans a link's repo-relative path and refuses one that
// would escape the checkout ("" — the repo root — always passes). A crafted
// link can carry ".." segments straight through to here (ParseLink checks the
// remote-named form's Path for the grammar's SEPARATORS, not for traversal,
// and linkMovedSplit performs no cleaning of its own); a resolved link must
// never point outside the checkout it names.
func cleanLinkRelPath(rel string) (string, error) {
	if rel == "" {
		return "", nil
	}
	// The escape check runs on the BACKSLASH-NORMALISED text: path.Clean knows
	// nothing about '\', so `..\..\x` would otherwise sail straight through it
	// and out of the checkout on Windows. The returned value is still cleaned
	// from the original, so a POSIX file whose name legitimately contains a
	// backslash keeps its name.
	guard := path.Clean(strings.ReplaceAll(rel, `\`, "/"))
	clean := path.Clean(rel)
	if clean == "." {
		return "", nil
	}
	if guard == ".." || strings.HasPrefix(guard, "../") || path.IsAbs(guard) ||
		clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", fmt.Errorf("%w: gg link path %q escapes the checkout", model.ErrLink, rel)
	}
	return clean, nil
}

// linkSplit reports whether abs is checkout itself or sits underneath it, and
// returns the git slash path of the remainder ("" when abs IS the checkout).
func linkSplit(abs, checkout string) (string, bool) {
	a := filepath.ToSlash(filepath.Clean(abs))
	c := filepath.ToSlash(filepath.Clean(checkout))
	if linkPathKey(a) == linkPathKey(c) {
		return "", true
	}
	pre := c
	if !strings.HasSuffix(pre, "/") {
		pre += "/"
	}
	if !strings.HasPrefix(linkPathKey(a), linkPathKey(pre)) {
		return "", false
	}
	return a[len(pre):], true
}

// linkMovedSplit finds base as a directory segment of abs (the LAST match
// wins) and returns everything after it, plus the PREFIX up to and including
// the matched segment — the absolute path the link believed the checkout was
// at, which the caller checks is really gone. It is the moved-checkout
// fallback: the recorded path is gone, so only the checkout's own directory
// name is left to match on.
func linkMovedSplit(abs, base string) (rel, prefix string, ok bool) {
	slash := filepath.ToSlash(abs)
	rooted := strings.HasPrefix(slash, "/")
	segs := strings.Split(strings.Trim(slash, "/"), "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if linkPathKey(segs[i]) != linkPathKey(base) {
			continue
		}
		// A Windows path's first segment is the drive ("C:"), which takes no
		// leading separator; a POSIX one keeps the root slash it came with.
		prefix = strings.Join(segs[:i+1], "/")
		if rooted {
			prefix = "/" + prefix
		}
		if i == len(segs)-1 {
			return "", prefix, true
		}
		return path.Join(segs[i+1:]...), prefix, true
	}
	return "", "", false
}

// linkNameEq compares two repository names. Case matters everywhere except
// Windows, where the filesystem does not distinguish them (spec §1).
// repos.NoRemote ("-", the memoised "this checkout has no remote") never
// matches anything: it is a sentinel, not a name. A remote URL ending in
// "/-.git" would be indistinguishable from it — accepted, since the cost is
// one such repository resolving by its absolute path instead of its name.
func linkNameEq(a, b string) bool {
	if a == "" || b == "" || a == repos.NoRemote || b == repos.NoRemote {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// linkPathKey normalises a path for comparison: slash form, and case-folded on
// the platforms whose filesystems are case-insensitive.
func linkPathKey(p string) string {
	s := filepath.ToSlash(p)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(s)
	}
	return s
}

// samePathLink reports whether two absolute paths name the same place.
func samePathLink(a, b string) bool {
	return linkPathKey(filepath.Clean(a)) == linkPathKey(filepath.Clean(b))
}

// SameCheckout reports whether two absolute checkout paths name the same
// place, by the SAME rule the link resolver uses to deduplicate candidates
// (slash-normalised, case-folded where the filesystem is). A frontend
// comparing LocateLink's answer against its own TopLevel must not invent a
// second rule: a byte compare would call a Windows link naming "C:/Src" a
// different repository from the checkout at "c:/src".
func SameCheckout(a, b string) bool { return samePathLink(a, b) }

// SamePath reports whether two absolute paths name the same place on this
// machine (filepath.Clean + slash-form + case-folded where the filesystem is
// case-insensitive). Exported so Task 6's CLI can reuse the exact rule this
// resolver uses instead of duplicating the normalisation.
func SamePath(a, b string) bool { return samePathLink(a, b) }

// linkRepoLabel names the link's repository half for an error message.
func linkRepoLabel(l model.Link) string {
	if l.IsLocal() {
		return l.Repo.Abs
	}
	return l.Repo.Name
}
