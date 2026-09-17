package domain

import (
	"context"
	"fmt"
	"sort"

	"github.com/homeend/gigagit/internal/model"
)

// FileSet is what a link evaluates to — the spec's §3.1 one rule made a type.
//
//	A PAIR is BOUNDED.   A POINT is UNBOUNDED.
//
// A bounded set carries an enumerated, sorted path list; an unbounded one
// stands for every file in the repository at some state and carries no list
// (enumerating a 100GB monorepo to compare one file against it is exactly what
// §3.5's "you can only ever scale DOWN" forbids).
//
// Either way the set carries a RESOLVED endpoint, which is the per-path byte
// source: fs.Endpoint().FileRef(p) → ResolveBytes. "Resolved" is load-bearing
// — EvalEndpoint turns an EndpointRef into an EndpointCommit, so no moving
// name survives into a comparison or a cache key.
type FileSet struct {
	ep      model.Endpoint
	paths   []string
	bounded bool // explicit, NOT len(paths) > 0: an empty bounded set is legal
	// has answers "does this member have BYTES at ep" for every enumerated
	// path. It is NOT redundant with paths (ruling R6): a change-set
	// enumerates the files it DELETED, and `git show <b>:<deleted>` cannot
	// read them. Without this the comparison asks for a file that is not
	// there and hard-errors instead of reporting A/D. nil ⇒ every member has
	// bytes, which is the shelf and whole-tree case.
	has map[string]bool
}

// Bounded reports whether the set enumerates its paths.
func (f FileSet) Bounded() bool { return f.bounded }

// Paths is the sorted key set, or nil when unbounded.
func (f FileSet) Paths() []string { return f.paths }

// Endpoint is the resolved byte source for a path in this set.
func (f FileSet) Endpoint() model.Endpoint { return f.ep }

// Has reports whether path has readable bytes at this set's endpoint. Only
// meaningful for a bounded set; the unbounded lane asks endpointPaths instead.
func (f FileSet) Has(path string) bool {
	if f.has == nil {
		return true
	}
	return f.has[path]
}

// boundedSetWith and unboundedSet are the only two constructors, so the
// invariant "bounded ⇒ paths is sorted and non-nil" holds by construction.
// A nil has means "every member has bytes".
func boundedSetWith(ep model.Endpoint, paths []string, has map[string]bool) FileSet {
	if paths == nil {
		paths = []string{}
	}
	sort.Strings(paths)
	return FileSet{ep: ep, paths: paths, bounded: true, has: has}
}

func unboundedSet(ep model.Endpoint) FileSet { return FileSet{ep: ep} }

// EvalEndpoint turns an endpoint into its file set (spec §4.2). It is the
// ONLY place an EndpointRef is allowed to die: a ref is resolved to a commit
// here, before the set — and therefore any cache key or git invocation built
// from it — can see it (plan 1b ruling R2).
//
// Deliberately NOT wrapped in one query(): each underlying read takes its own
// Read reservation, and nesting a gated read inside a held reservation can
// deadlock behind a queued writer. shelfCompareFiles's doc comment records the
// same rule.
func (s *Service) EvalEndpoint(ctx context.Context, e model.Endpoint) (FileSet, error) {
	switch e.Kind() {
	case model.EndpointWorkTree, model.EndpointIndex, model.EndpointCommit:
		return unboundedSet(e), nil

	case model.EndpointRef:
		sha, ok, err := s.ResolveRev(ctx, e.Ref())
		if err != nil {
			return FileSet{}, err
		}
		if !ok {
			return FileSet{}, fmt.Errorf("unknown revision %q", e.Ref())
		}
		commit, err := model.CommitEndpoint(sha)
		if err != nil {
			return FileSet{}, err
		}
		return unboundedSet(commit), nil

	case model.EndpointPair:
		a, err := model.CommitEndpoint(e.PairA())
		if err != nil {
			return FileSet{}, err
		}
		b, err := model.CommitEndpoint(e.PairB())
		if err != nil {
			return FileSet{}, err
		}
		files, err := s.CompareFiles(ctx, a, b)
		if err != nil {
			return FileSet{}, err
		}
		// The set's byte source is the pair's NEW side: a member's content
		// means "as it is at b". A member the pair DELETED has NO bytes at b,
		// so it is enumerated with has[path] = false and CompareSets reports
		// it through A/D instead of trying to read it (ruling R6).
		//
		// KNOWN GAP (1b): DiffTreeFiles passes -M, so a rename arrives as one
		// "R" row carrying the NEW path only; the old path is not enumerated.
		// A renamed file therefore compares as an addition on this side. That
		// matches what `git diff --name-status` reports and is left as-is.
		paths := make([]string, 0, len(files))
		has := make(map[string]bool, len(files))
		for _, f := range files {
			paths = append(paths, f.Path)
			has[f.Path] = f.Status != "D"
		}
		return boundedSetWith(b, paths, has), nil

	case model.EndpointShelf:
		members, err := s.ShelfCommitFiles(ctx, e.ShelfID())
		if err != nil {
			return FileSet{}, err
		}
		// Every tar member has bytes, so has stays nil.
		paths := make([]string, 0, len(members))
		for _, f := range members {
			paths = append(paths, f.Path)
		}
		return boundedSetWith(e, paths, nil), nil

	default:
		return FileSet{}, fmt.Errorf("EvalEndpoint: unusable endpoint kind %d", e.Kind())
	}
}

// endpointPaths is the MEMBER SET of an unbounded endpoint — the probe
// "does this tree contain <path>". It exists only for the unbounded × bounded
// lane of CompareSets, where a key absent from the tree reads as A or D
// (spec §3.5).
//
// One listing, not one probe per path: a bounded side may hold hundreds of
// members, and `git cat-file -e` per path would be hundreds of invocations.
func (s *Service) endpointPaths(ctx context.Context, e model.Endpoint) (map[string]bool, error) {
	var list []string
	switch e.Kind() {
	case model.EndpointCommit:
		files, err := s.TreeFiles(ctx, e.Hash())
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			list = append(list, f.Path)
		}
	case model.EndpointIndex:
		var err error
		if list, err = s.ListFiles(ctx, false); err != nil {
			return nil, err
		}
	case model.EndpointWorkTree:
		// tracked ∪ untracked − deleted. The subtraction matters: ls-files
		// lists the INDEX, so a tracked file the user `rm`'d still appears
		// there, and calling it "present" would send ResolveBytes after a
		// file that is not on disk.
		tracked, err := s.ListFiles(ctx, false)
		if err != nil {
			return nil, err
		}
		untracked, err := s.UntrackedFiles(ctx)
		if err != nil {
			return nil, err
		}
		gone, err := s.ListFiles(ctx, true)
		if err != nil {
			return nil, err
		}
		removed := make(map[string]bool, len(gone))
		for _, p := range gone {
			removed[p] = true
		}
		// Never `append(tracked, untracked...)`: tracked is the slice a
		// singleflight read handed out and other callers may hold it, so
		// filling its spare capacity would scribble on theirs.
		for _, src := range [][]string{tracked, untracked} {
			for _, p := range src {
				if !removed[p] {
					list = append(list, p)
				}
			}
		}
	case model.EndpointShelf, model.EndpointPair:
		// A bounded endpoint never reaches here: CompareSets asks for the
		// member set only of the side its own Bounded() said is unbounded.
		return nil, fmt.Errorf("endpointPaths: %d is not an unbounded endpoint", e.Kind())
	case model.EndpointRef:
		// A ref IS unbounded, but it moves: EvalEndpoint resolved it to a
		// commit long before anything asked for a member set, so one arriving
		// here means a caller skipped that step.
		return nil, fmt.Errorf("endpointPaths: ref %q was never resolved to a commit — call EvalEndpoint first", e.Ref())
	default:
		return nil, fmt.Errorf("endpointPaths: unknown endpoint kind %d", e.Kind())
	}
	set := make(map[string]bool, len(list))
	for _, p := range list {
		set[p] = true
	}
	return set, nil
}
