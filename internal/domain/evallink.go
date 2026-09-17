package domain

import (
	"context"
	"fmt"
	"slices"

	"github.com/homeend/gigagit/internal/model"
)

// EndpointForLink is the ONE answer to "what does this link address" — the
// pairing rules that used to live in internal/cli/compare.go, moved here so
// the TUI, CLI, MCP and web cannot disagree (spec §4.2).
//
// The hint is IGNORED here, deliberately: spec §3.3 rule 1 says compare
// ignores it, and that is what keeps the algebra a 2×2 instead of a
// bookmark × shelf × commit matrix. A `?bookmark=` hint therefore NEVER
// changes the answer.
//
// A `?shelf=` hint has the spec's two named exceptions, and only those two —
// both because a shelf entry can be the only place the BYTES survive (spec
// §3.3's exception table, §6):
//
//  1. NO ADDRESS at all (a shelved working-tree file, whose bytes were never
//     in git) ⇒ the shelf endpoint, not the working tree.
//  2. A COMMIT address whose sha no longer resolves ⇒ the frozen tar, through
//     ResolveCommitEntryEndpoint's hybrid. While the sha resolves this is the
//     plain commit endpoint, so rule 1 still holds for every live link.
//
// Neither exception makes the hint an identity: both are fallbacks for bytes
// with nowhere else to come from.
//
// Side, Line and Hunk are ignored too: they are where a link LANDS, not what
// it addresses. Narrowing to a link's /<path> is EvalLink's job, not this
// one's — an endpoint names a byte source, never a key set.
//
// THE RESULT MAY BE AN UNRESOLVED EndpointRef, and a ref MOVES: this is the
// answer to "what does this link address", which for "@ref:main" is the
// branch, not whichever commit it happens to sit on. Nothing that COMPARES or
// CACHES may take it as it stands — Endpoint.CacheTag panics on a ref by
// design, so a moving name cannot become a cache key. EvalEndpoint is the one
// place a ref is allowed to die (plan 1b ruling R2), which is why EvalLink
// always goes through it. A caller wanting only to DISPLAY what a link
// addresses may use this directly; any other caller resolves first.
func (s *Service) EndpointForLink(ctx context.Context, l model.Link) (model.Endpoint, error) {
	t := l.Target

	// The address-less shelf link: the hint is the content (spec §3.3). A
	// live, uncommitted state (Unstaged OR Staged — fix F6: this used to
	// read only `t.State == model.StateUnstaged`, which missed
	// `gg://<repo>@staged?shelf=X`) with no pinned target at all
	// (hintOnlyTarget, linkresolve.go — shared with finishLink's own
	// address-less check) substitutes the STABLE shelf snapshot for the
	// live working tree/index; deliberately independent of l.Path — see
	// hintOnlyTarget's doc comment for why a real path does not disqualify
	// this arm.
	if (t.State == model.StateUnstaged || t.State == model.StateStaged) && hintOnlyTarget(t) && l.Hint.Kind == "shelf" {
		return model.ShelfEndpoint(l.Hint.ID)
	}

	switch t.State {
	case model.StateUnstaged:
		return model.WorkTreeEndpoint(), nil
	case model.StateStaged:
		return model.IndexEndpoint(), nil
	case model.StateCommitted:
		// Fall through to the committed forms below. StateCommitted is
		// FileState's ZERO value, so this arm is also every hand-built Link
		// nobody filled in — which is why the switch below still ends in an
		// explicit refusal rather than a default commit.
	}

	switch {
	case t.Ref != "":
		return model.RefEndpoint(t.Ref)

	case t.Pair != nil:
		a, err := s.resolveHalf(ctx, t.Pair.A)
		if err != nil {
			return model.Endpoint{}, err
		}
		b, err := s.resolveHalf(ctx, t.Pair.B)
		if err != nil {
			return model.Endpoint{}, err
		}
		return model.PairEndpoint(a, b)

	case t.Preview != nil:
		// Three dots mean merge-base(target, source)..source — the shipped
		// preview vocabulary (internal/cli/preview.go). The base is a
		// RESOLUTION, which is why the endpoint holds two shas and no
		// three-dot flag (plan 1b ruling R1).
		//
		// Both names are resolved to shas BEFORE the merge base, exactly as
		// PreviewSummary does it: a name git cannot resolve then reports as
		// "unknown revision" instead of arriving here as a merge-base refusal
		// and being mistaken for unrelated histories.
		tgt, err := s.resolveHalf(ctx, t.Preview.Target)
		if err != nil {
			return model.Endpoint{}, err
		}
		src, err := s.resolveHalf(ctx, t.Preview.Source)
		if err != nil {
			return model.Endpoint{}, err
		}
		base, err := s.mergeBase(ctx, tgt, src)
		if err != nil {
			return model.Endpoint{}, err
		}
		// base == src when the source is fully merged. model.PairEndpoint
		// calls that legal (ruling R7): the link evaluates to the EMPTY
		// change-set, which is a result, not an error.
		return model.PairEndpoint(base, src)

	case t.Commit != "":
		// THE HINT'S SECOND AND LAST EXCEPTION to rule 1 (the first is the
		// address-less form above). Spec §3.3's table row 1 and §6's
		// "shelved commit gc'd, frozen tar present → use the tar" both say a
		// shelf-hinted commit reads live WHILE THE SHA RESOLVES and falls back
		// to the frozen tar once it does not — so the hint is not a second
		// identity here, it is a FALLBACK byte source for an address that has
		// died. That is exactly ResolveCommitEntryEndpoint's hybrid, which is
		// what `gg compare shelf:<id>` already goes through; routing the link
		// spelling anywhere else made two spellings of one entry answer
		// differently, the second with git's raw "fatal: bad object".
		//
		// Rule 1 is intact: while the commit exists this returns the same
		// CommitEndpoint an unhinted link would, so a bookmarked commit and
		// the same commit off the log stay ONE endpoint. Do not "simplify"
		// this back to model.CommitEndpoint.
		if l.Hint.Kind == "shelf" {
			return s.ResolveCommitEntryEndpoint(ctx, t.Commit, l.Hint.ID)
		}
		return model.CommitEndpoint(t.Commit)
	}
	return model.Endpoint{}, fmt.Errorf("%w: the link addresses nothing comparable", model.ErrLink)
}

// mergeBase is merge-base(a, b) under its own Read reservation, shaped like
// CompareOrigins' call (compare_origins.go) so there is one way to ask this
// question. A genuine refusal means unrelated histories and maps to the
// existing ErrNoMergeBase — a caller keeps one errors.Is target for "these two
// revisions share no ancestor", whether it asked through CompareOrigins or
// through a preview link.
//
// A CANCELLATION propagates untouched rather than being folded into
// ErrNoMergeBase: an aborted read is not a statement about the two histories.
// (CompareOrigins wraps the cause with %v and loses that distinction; this is
// PreviewSummary's guard, which does not.)
func (s *Service) mergeBase(ctx context.Context, a, b string) (string, error) {
	key := "merge-base:" + a + ":" + b
	return query(ctx, s, key, func(ctx context.Context) (string, error) {
		base, err := s.repo.MergeBase(ctx, a, b)
		if err != nil {
			if ctx.Err() != nil {
				return "", err
			}
			return "", fmt.Errorf("%w: %v", ErrNoMergeBase, err)
		}
		return base, nil
	})
}

// resolveHalf turns one half of a pair — a sha or a refname — into a full
// object id. Full, never `%h`: a short sha honours core.abbrev (legal down to
// 4) and model.CommitEndpoint requires 7..64, so a short resolver turns a
// legal repo config into a hard failure. That regression is plan 1a's third
// bug; do not reintroduce it by reaching for CommitLookup.
func (s *Service) resolveHalf(ctx context.Context, rev string) (string, error) {
	sha, ok, err := s.ResolveRev(ctx, rev)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return sha, nil
}

// EvalLink is the whole pipeline of spec §4.1 in one call:
//
//	Link → Endpoint → FileSet
//
// A /<path> narrows the set to that ONE member, whatever the target was: the
// spec's last grammar row, and the reason "compare one file against a commit"
// needs no special case anywhere else.
//
// PRECONDITION: the link's repository and path are already split. A PARSED
// LOCAL-form link (gg:///abs/checkout/dir/file.go@…) carries the checkout AND
// the file undivided in Repo.Abs with Path empty — only this machine's
// repository registry can divide them, which is ResolveLink's job. Handed one
// of those, this reads a whole-tree link and silently drops the file, so a
// frontend resolves a local-form link before evaluating it.
func (s *Service) EvalLink(ctx context.Context, l model.Link) (FileSet, error) {
	ep, err := s.EndpointForLink(ctx, l)
	if err != nil {
		return FileSet{}, err
	}
	fs, err := s.EvalEndpoint(ctx, ep)
	if err != nil {
		return FileSet{}, err
	}
	if l.Path != "" {
		return s.narrowTo(ctx, fs, l.Path)
	}
	return fs, nil
}

// narrowTo bounds a set to ONE path — the spec's last grammar row, a link
// with a /<path>. It only ever scales DOWN (spec §3.5): the endpoint is
// carried over untouched, so the member's bytes still come from wherever the
// original set's did.
//
// The narrowed set still has to answer "does that path have bytes here"
// (ruling R6), or a link naming a file that does not exist at its target
// would send a byte read after nothing instead of reporting A/D. A set that is
// already bounded knows; an unbounded one is asked once, through the same
// probe CompareSets would use — for the one path only, never the whole tree.
//
// MEMBERSHIP IS PART OF THAT ANSWER on the bounded side, and not because a
// pair needs it (a pair's has map is non-nil, so a non-member already misses
// to false) but because a SHELF's is nil, meaning "every member has bytes" —
// so Has() answers true for any path, member or not. compareBoundedPair
// documents the same trap and guards it the same way.
func (s *Service) narrowTo(ctx context.Context, fs FileSet, path string) (FileSet, error) {
	narrowed := func(has bool) FileSet {
		out := boundedSetWith(fs.Endpoint(), []string{path}, map[string]bool{path: has})
		// The ONE thing the endpoint cannot say afterwards: this set is a
		// projection, not the endpoint's own set. FileSet.Narrowed's doc names
		// the consumer that needs to know.
		out.narrowed = true
		return out
	}
	if fs.Bounded() {
		return narrowed(fs.Has(path) && slices.Contains(fs.Paths(), path)), nil
	}
	present, err := s.endpointHas(ctx, fs.Endpoint(), []string{path})
	if err != nil {
		return FileSet{}, err
	}
	return narrowed(present[path]), nil
}
