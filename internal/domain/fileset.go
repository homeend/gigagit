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
	// narrowed records that this set is a PROJECTION of its endpoint's own
	// set rather than the whole of it — a link with a /<path> (narrowTo).
	// It is not derivable from ep, which narrowTo carries over untouched, and
	// it is the one thing a consumer that re-derives sets FROM the endpoint
	// (domain.ComparePatch's shelf lane) cannot reconstruct. Frontends ask
	// Narrowed() before handing a set to such a consumer.
	narrowed bool
	// has answers "does this member have BYTES at ep" for every enumerated
	// path. It is NOT redundant with paths (ruling R6): a change-set
	// enumerates the files it DELETED, and `git show <b>:<deleted>` cannot
	// read them. Without this the comparison asks for a file that is not
	// there and hard-errors instead of reporting A/D. nil ⇒ every member has
	// bytes, which is the shelf and whole-tree case.
	has map[string]bool
	// src overrides the byte source for individual members; nil (or a miss)
	// means ep. It exists for ONE shape today — a `-u` stash, whose untracked
	// files live in the stash commit's THIRD parent rather than in its own
	// tree (spec §3.4) — and is why a member's bytes are read through
	// Source(path), never through Endpoint() directly.
	src map[string]model.Endpoint
}

// Bounded reports whether the set enumerates its paths.
func (f FileSet) Bounded() bool { return f.bounded }

// Paths is the sorted key set, or nil when unbounded. It returns a COPY:
// FileSet is exported and crosses into the frontends, and this repo has
// already shipped one bug from a caller sorting a slice domain still owned.
func (f FileSet) Paths() []string {
	if f.paths == nil {
		return nil
	}
	out := make([]string, len(f.paths))
	copy(out, f.paths)
	return out
}

// Endpoint is the resolved byte source for a path in this set.
//
// It is NOT the set's boundedness, and the two deliberately disagree: a PAIR
// set is bounded, but its endpoint is commit b — a point, so
// fs.Endpoint().Bounded() is false while fs.Bounded() is true. Ask the SET
// whether it enumerates its paths; ask the ENDPOINT only for bytes.
func (f FileSet) Endpoint() model.Endpoint { return f.ep }

// Source is the resolved byte source for ONE member: the set's endpoint unless
// this path is overridden (FileSet.src). Every byte read of a member goes
// through here; Endpoint() answers "which endpoint is this set", not "where do
// this path's bytes live".
func (f FileSet) Source(path string) model.Endpoint {
	if ep, ok := f.src[path]; ok {
		return ep
	}
	return f.ep
}

// Narrowed reports whether this set is a PROJECTION of its endpoint's own
// file set (a link's /<path>) rather than the whole of it.
//
// It exists for one question: may this set be handed to a consumer that
// re-derives the sets from the ENDPOINTS instead of taking them? ComparePatch
// does exactly that on its shelf lane, so a narrowed set given to it would
// silently widen back to every member of the tar.
func (f FileSet) Narrowed() bool { return f.narrowed }

// Has reports whether path has readable bytes at this set's endpoint. Only
// meaningful for a bounded set; the unbounded lane asks endpointHas instead.
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

// boundedSetFull is boundedSetWith plus per-path byte sources (nil = none).
func boundedSetFull(ep model.Endpoint, paths []string, has map[string]bool, src map[string]model.Endpoint) FileSet {
	fs := boundedSetWith(ep, paths, has)
	if len(src) > 0 {
		fs.src = src
	}
	return fs
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
	case model.EndpointWorkTree, model.EndpointIndex:
		return unboundedSet(e), nil

	case model.EndpointCommit:
		// Normalize an ABBREVIATED sha to its full one. Two abbreviations of
		// one commit are two different CacheTags, so leaving them fragments
		// the compare cache and probes the same tree twice. A full sha (40 for
		// sha-1, 64 for sha-256) is passed through untouched, so the common
		// path costs no git invocation.
		if l := len(e.Hash()); l != 40 && l != 64 {
			sha, ok, err := s.ResolveRev(ctx, e.Hash())
			if err != nil {
				return FileSet{}, err
			}
			if !ok {
				return FileSet{}, fmt.Errorf("unknown commit %q", e.Hash())
			}
			full, err := model.CommitEndpoint(sha)
			if err != nil {
				return FileSet{}, err
			}
			return unboundedSet(full), nil
		}
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
		// A RENAME is two members, not one: DiffTreeFiles passes -M, so it
		// arrives as a single "R" row carrying both paths, and the old path is
		// as gone from b as any deletion. Enumerating only the new path would
		// make a rename compare as an addition with no matching deletion.
		// A COPY ("C") is different — its old path still exists at b — so only
		// R contributes the extra member.
		paths := make([]string, 0, len(files))
		has := make(map[string]bool, len(files))
		for _, f := range files {
			paths = append(paths, f.Path)
			has[f.Path] = f.Status != "D"
			if f.Status == "R" && f.OldPath != "" {
				paths = append(paths, f.OldPath)
				has[f.OldPath] = false
			}
		}
		// A `-u` stash keeps its untracked files in a THIRD, parentless
		// parent, which a..b cannot see (spec §3.4). They join the set as
		// members whose bytes are read from that parent. A path the tracked
		// diff already names keeps its own row: the tracked answer wins.
		shape, isStash, err := s.stashShape(ctx, e.PairA(), e.PairB())
		if err != nil {
			return FileSet{}, err
		}
		var src map[string]model.Endpoint
		if isStash {
			untracked, err := model.CommitEndpoint(shape.Untracked)
			if err != nil {
				return FileSet{}, err
			}
			ufiles, err := s.TreeFiles(ctx, shape.Untracked)
			if err != nil {
				return FileSet{}, err
			}
			src = make(map[string]model.Endpoint, len(ufiles))
			for _, f := range ufiles {
				if _, member := has[f.Path]; member {
					continue
				}
				paths = append(paths, f.Path)
				has[f.Path] = true
				src[f.Path] = untracked
			}
		}
		return boundedSetFull(b, paths, has, src), nil

	case model.EndpointShelf:
		// A shelf entry is one of TWO things, and the entry kind is the only
		// discriminator (shelfResolve makes the same split for bytes):
		//
		//   commit entry — a tar of the paths the shelved commit changed.
		//   FILE entry   — one blob captured from the working tree or index,
		//                  whose bytes may correspond to nothing in git. This
		//                  is spec §3.3's exception: the address-less shelf
		//                  link, the one link in the system with no address at
		//                  all, and the reason EndpointForLink has an arm for
		//                  it. Routing it through ShelfCommitFiles (which
		//                  refuses a non-commit entry) made that link
		//                  buildable and un-evaluatable.
		entry, err := s.ShelfFind(ctx, e.ShelfID())
		if err != nil {
			return FileSet{}, err
		}
		if !entry.IsCommit() {
			// ONE member: the path the blob was captured from. Refused rather
			// than guessed when the record carries none — a set whose single
			// key is "" would compare the whole tree against one blob.
			if entry.Origin.Path == "" {
				return FileSet{}, fmt.Errorf("shelf: entry %s records no origin path, so it names no file", e.ShelfID())
			}
			// has stays nil: the blob IS the member's bytes, so it always has
			// some — the same "every member has bytes" the tar case relies on.
			return boundedSetWith(e, []string{entry.Origin.Path}, nil), nil
		}
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

// pathProbeBatch caps how many paths go into one pathspec. A bounded side can
// hold thousands of members, and every OS bounds a process argv (Linux's
// MAX_ARG_STRLEN/ARG_MAX, Windows' ~32k command line) — a single unbatched
// invocation would fail outright on a large change-set. A few hundred keeps
// the argv far under every limit while holding the invocation count to
// members/batch.
const pathProbeBatch = 256

// endpointHas answers "does this path have readable bytes at this endpoint"
// for the paths a BOUNDED side supplies — never for the whole repository.
//
// It takes the key set because the unbounded side must never be enumerated: a
// ls-tree of a ~100GB monorepo's head, per comparison, to answer a question
// about three files is precisely the "you can only ever scale DOWN" rule the
// design is built on (spec §3.5). Every probe below is limited to the keys.
//
// The result holds an entry for every input path and for NO other path: the
// git verbs wrap each pathspec element in `:(literal)`, so a key holding a
// glob metacharacter cannot drag an unasked-for path into the answer, and a
// key beginning with ':' cannot be eaten as pathspec magic and read absent.
//
// Deliberately NOT wrapped in one query(): each probe takes its own Read
// reservation, and nesting a gated read inside a held reservation can deadlock
// behind a queued writer.
func (s *Service) endpointHas(ctx context.Context, e model.Endpoint, paths []string) (map[string]bool, error) {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		out[p] = false
	}
	if len(paths) == 0 {
		return out, nil
	}

	switch e.Kind() {
	case model.EndpointCommit:
		for _, batch := range batchPaths(paths) {
			found, err := s.TreePaths(ctx, e.Hash(), batch)
			if err != nil {
				return nil, err
			}
			for _, p := range found {
				out[p] = true
			}
		}
		return out, nil

	case model.EndpointIndex:
		for _, batch := range batchPaths(paths) {
			found, err := s.LsFiles(ctx, batch...)
			if err != nil {
				return nil, err
			}
			for _, p := range found {
				out[p] = true
			}
		}
		return out, nil

	case model.EndpointWorkTree:
		// No git here at all: the question is "is this file on disk", and no
		// git listing answers it — `ls-files --deleted` cannot see a
		// skip-worktree entry, so on a sparse checkout it would call every
		// sparse-excluded path present. The filesystem is the authority.
		// No batching either: a stat takes no argv.
		//
		// COPIED into out, like the two arms above, rather than handed back as
		// it comes. WorktreeFilesPresent goes through query(), so concurrent
		// callers that coalesce on one flight all receive the LEADER'S map
		// header — returning it directly would make every caller's "own"
		// result the same object, and a map is the easiest thing here to write
		// into by accident. sortedCompareRows' doc records the same hazard for
		// the slice half of the algebra.
		found, err := s.WorktreeFilesPresent(ctx, paths)
		if err != nil {
			return nil, err
		}
		for p, ok := range found {
			out[p] = ok
		}
		return out, nil

	case model.EndpointRef:
		// Unreachable: EvalEndpoint resolves a ref to a commit before any set
		// — and therefore any probe built from one — can see it. An explicit
		// arm rather than a silent default, so this stays a loud bug report.
		return nil, fmt.Errorf("endpointHas: ref %q was never resolved to a commit — call EvalEndpoint first", e.Ref())

	case model.EndpointShelf, model.EndpointPair:
		// A bounded side answers presence from its own FileSet.Has map, which
		// EvalEndpoint already built. Routing it through here would mean
		// enumerating it a second time.
		return nil, fmt.Errorf("endpointHas: %d is bounded — ask its FileSet.Has instead", e.Kind())

	default:
		return nil, fmt.Errorf("endpointHas: unknown endpoint kind %d", e.Kind())
	}
}

// batchPaths splits paths into pathProbeBatch-sized chunks that share the
// input's backing array (read-only use only — every caller just passes them to
// a pathspec).
func batchPaths(paths []string) [][]string {
	var out [][]string
	for i := 0; i < len(paths); i += pathProbeBatch {
		end := min(i+pathProbeBatch, len(paths))
		out = append(out, paths[i:end])
	}
	return out
}
