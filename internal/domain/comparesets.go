package domain

import (
	"bytes"
	"context"
	"sort"

	"github.com/homeend/gigagit/internal/model"
)

// CompareSets is the spec's §3.5 algebra, and the whole of it:
//
//	unbounded × unbounded  →  BOUNDED   git diff A B: the differing paths
//	bounded   × bounded    →  BOUNDED   symmetric over the union of members;
//	                                    present in one only ⇒ A / D
//	unbounded × bounded    →  BOUNDED   project the tree onto the bounded
//	                                    side's paths; a key absent from the
//	                                    tree ⇒ A / D
//
// THE ASYMMETRY IS LOAD-BEARING: you can only ever scale DOWN. Growing the
// bounded side to the tree's size would mean comparing one file against every
// file in a 100GB monorepo, so the bounded side always supplies the key set.
//
// Every row yields a bounded result, which is what makes compare CLOSED: a
// result is the same KIND as a merge preview, so it is saveable, linkable and
// usable as an endpoint of the next comparison.
//
// Statuses follow the tree-diff convention the whole codebase already uses:
// only-in-left → D, only-in-right → A, differing bytes → M, identical →
// omitted. Results are sorted by path.
//
// One asymmetry between the lanes is worth knowing: the unbounded × unbounded
// lane hands back whatever CompareFiles produced, which is a git diff-tree
// listing and can therefore carry "R"/"C" rows with an OldPath. The two
// projected lanes decide each path themselves and emit only A/D/M.
//
// Deliberately NOT wrapped in one query(): each underlying read takes its own
// Read reservation, and nesting a gated read inside a held reservation can
// deadlock behind a queued writer.
func (s *Service) CompareSets(ctx context.Context, left, right FileSet) ([]model.CommitFile, error) {
	switch {
	case !left.Bounded() && !right.Bounded():
		// Both points: git already answers this in one invocation, INCLUDING
		// the untracked-file handling a working-tree side needs. Reuse it
		// rather than re-deriving it here.
		if left.Endpoint() == right.Endpoint() {
			// The same point on both sides. git has no argv for `diff
			// @worktree @worktree`, and there is nothing to ask: a text differs
			// from itself nowhere.
			return nil, nil
		}
		if forwardLivePair(left.Endpoint(), right.Endpoint()) {
			files, err := s.CompareFiles(ctx, left.Endpoint(), right.Endpoint())
			if err != nil {
				return nil, err
			}
			// SORTED, like every other lane and like this function's own doc
			// promises. CompareFiles hands back git's order with untracked
			// files appended, so without this `gg compare main @worktree`
			// printed "M z.txt" before "A a.txt" while the reverse direction of
			// the same pair came out sorted — one comparison, two orders.
			//
			// sortedCompareRows COPIES rather than sorting in place; its doc
			// says why, and invertCompareRows copies for the same reason.
			return sortedCompareRows(files), nil
		}
		// git's own diff only walks FORWARD (a commit, then the index, then the
		// working tree — DiffTreeFiles' four supported pairs), so the reverse of
		// one of those is asked forward and the answer turned round. THIS is
		// what makes the algebra total, and it is why `gg compare @worktree main`
		// is a comparison and no longer a usage error: the ordering rule that
		// used to live in internal/cli (validComparePair) was git's limitation
		// showing through, not a statement about what the user may ask.
		files, err := s.CompareFiles(ctx, right.Endpoint(), left.Endpoint())
		if err != nil {
			return nil, err
		}
		return invertCompareRows(files), nil

	case left.Bounded() && right.Bounded():
		return s.compareBoundedPair(ctx, left, right)

	case right.Bounded():
		// unbounded × bounded: the RIGHT side supplies the keys, so a key the
		// left (unbounded) side lacks reads as ADDED.
		return s.compareProjected(ctx, left, right, right)

	default:
		// bounded × unbounded: the LEFT side supplies the keys, so a key the
		// right (unbounded) side lacks reads as DELETED.
		return s.compareProjected(ctx, left, right, left)
	}
}

// forwardLivePair reports whether (left, right) is one of the four pairs
// git's own diff can walk — the set DiffTreeFiles and livePairSpec enumerate:
// commit↔commit, commit→index, commit→worktree, index→worktree. Anything else
// over two unbounded points is that set's mirror image, which CompareSets asks
// forward and inverts.
func forwardLivePair(left, right model.Endpoint) bool {
	switch {
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointCommit:
		return true
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointIndex:
		return true
	case left.Kind() == model.EndpointCommit && right.Kind() == model.EndpointWorkTree:
		return true
	case left.Kind() == model.EndpointIndex && right.Kind() == model.EndpointWorkTree:
		return true
	}
	return false
}

// sortedCompareRows returns a path-sorted COPY, and never sorts its input.
//
// THE COPY IS THE POINT, and the reason is narrower than "there is a cache" —
// flightGroup keeps no cache at all; it frees the key as soon as the leader
// returns (internal/domain/flight.go). The hazard is CONCURRENT coalescing:
// callers that arrive while one key is in flight all receive the LEADER'S
// SLICE HEADER, the same backing array, from `query`. So if one of them
// post-processes that slice in place — and a sort is the purest example — it
// silently reorders the result its co-waiters are already holding. A `gg web`
// page and an `gg mcp` tool asking for the same comparison at the same moment
// is exactly that shape.
//
// TestSortedCompareRowsDoesNotMutateItsInput pins this — a unit test of the
// helper's contract, which is the cheap half.
//
// The SHARING itself is provable too, and fileset_test.go's
// TestEndpointHasWorktreeResultIsNotShared does it: take an exclusive
// repogate reservation first, launch the leader, and poll Gate.Queue() until
// it is parked as a waiter — at that point it is inside flightGroup.Do's fn
// and still holding the key, so a second caller on the same key can only be a
// follower. (An earlier version of this comment said no deterministic hook
// existed. You cannot observe the FOLLOWER joining, but you can observe the
// LEADER parking, which is enough.) Worth doing for this helper too if
// anybody touches it.
func sortedCompareRows(files []model.CommitFile) []model.CommitFile {
	out := make([]model.CommitFile, len(files))
	copy(out, files)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// invertCompareRows turns a forward diff listing round, so a reversed pair
// reads as the comparison the caller actually asked for.
//
//	A ⇄ D   the path exists on one side only, and which side just swapped
//	M, T    a modification and a type change are symmetric
//	R       a rename's two paths swap: what was renamed away is renamed back
//
// "C" (copy) cannot appear: DiffTreeFiles passes -M, never -C, so git never
// reports a copy. An unknown status is passed through untouched rather than
// guessed at — no status this codebase produces reaches that branch, and a
// silent remap would be a wrong answer where a verbatim one is merely unhelpful.
//
// The untracked-file add-on CompareFiles performs for a working-tree RIGHT
// side inverts correctly: those arrive "A" (on disk, absent from the older
// side) and come out "D", which is what a comparison ending at a commit means.
func invertCompareRows(files []model.CommitFile) []model.CommitFile {
	out := make([]model.CommitFile, 0, len(files))
	for _, f := range files {
		switch f.Status {
		case "A":
			f.Status = "D"
		case "D":
			f.Status = "A"
		case "R":
			f.Path, f.OldPath = f.OldPath, f.Path
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// compareBoundedPair walks the UNION of two enumerated sets. A path in only
// one of them is A or D by which side holds it; a path in both is compared by
// bytes.
func (s *Service) compareBoundedPair(ctx context.Context, left, right FileSet) ([]model.CommitFile, error) {
	rightPaths := right.Paths()
	inRight := make(map[string]bool, len(rightPaths))
	for _, p := range rightPaths {
		inRight[p] = true
	}
	leftPaths := left.Paths()
	inLeft := make(map[string]bool, len(leftPaths))
	var out []model.CommitFile
	for _, p := range leftPaths {
		inLeft[p] = true
		// Membership is not presence (ruling R6): a change-set ENUMERATES the
		// files it deleted, and those have no bytes at its endpoint. Decide
		// A/D/M from presence on both sides, and only read bytes when both
		// sides actually have some.
		//
		// The inRight[p] term is NOT redundant with right.Has(p): a set built
		// with a nil has map (the shelf case, "every member has bytes")
		// answers Has(p) true for ANY path, member or not.
		row, err := s.compareOne(ctx, left, right, p, left.Has(p), inRight[p] && right.Has(p))
		if err != nil {
			return nil, err
		}
		if row.Status != "" {
			out = append(out, row)
		}
	}
	for _, p := range rightPaths {
		if inLeft[p] {
			continue // already decided by the left pass above
		}
		// Routed through compareOne like every other path, even though the
		// left side provably has no bytes here: compareOne's doc promises it
		// is the ONLY place a status is decided, and an inline "A" would make
		// that promise false and the two lanes free to drift.
		row, err := s.compareOne(ctx, left, right, p, false, right.Has(p))
		if err != nil {
			return nil, err
		}
		if row.Status != "" {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// compareProjected is the unbounded × bounded lane. keys is whichever of
// left/right is the bounded one — it supplies the key set, always, because
// §3.5 says you may only ever scale DOWN. The unbounded side is never
// enumerated: endpointHas is asked only about the keys.
//
// The old shelfCommitCompare took a shelfIsRight flag to decide whether a
// missing key read A or D. That flag is gone: compareOne derives it from WHICH
// SIDE holds the path, which is the same answer and cannot be passed wrong.
func (s *Service) compareProjected(ctx context.Context, left, right, keys FileSet) ([]model.CommitFile, error) {
	unbounded := left
	if !right.Bounded() {
		unbounded = right
	}
	paths := keys.Paths()
	present, err := s.endpointHas(ctx, unbounded.Endpoint(), paths)
	if err != nil {
		return nil, err
	}
	// Each side answers presence its own way: the unbounded side from the
	// probe above, the bounded side from its own has map.
	var out []model.CommitFile
	for _, p := range paths {
		onLeft := (left.Bounded() && left.Has(p)) || (!left.Bounded() && present[p])
		onRight := (right.Bounded() && right.Has(p)) || (!right.Bounded() && present[p])
		row, err := s.compareOne(ctx, left, right, p, onLeft, onRight)
		if err != nil {
			return nil, err
		}
		if row.Status != "" {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// compareOne is the single A/D/M decision, given whether path has bytes on
// each side. It is the ONLY place that decides a status, so the two lanes
// above cannot drift apart:
//
//	left only   → D      right only  → A
//	neither     → omitted (nothing to say: it is in the key set because the
//	             bounded side enumerates it, but nobody holds bytes)
//	both        → read both and compare; identical ⇒ omitted
//
// A zero CommitFile (Status == "") means "omit this path".
//
// Three inherited imperfections in the presence answer land here, and none is
// guarded because none can produce a WRONG status silently:
//
//   - an UNMERGED path reads present from the index lane (`ls-files` prints it
//     once per stage) though `git show :path` refuses it, and
//   - a submodule GITLINK reads present from `ls-tree` though it has no
//     readable bytes.
//
// Both therefore fail loudly inside sameBytes, with the store's own error —
// which is the spec's error-handling rule (an unreadable side surfaces as-is),
// not a silent misreport.
//
//   - os.Lstat is case-insensitive on macOS and Windows where git is not, so
//     `README.MD` reads present in the working-tree lane. This one CAN be
//     silent (the byte read is case-insensitive too, so the path compares
//     identical and is omitted), but it is the platform-wide git/filesystem
//     case mismatch, not something this lane introduces.
func (s *Service) compareOne(ctx context.Context, left, right FileSet, path string, onLeft, onRight bool) (model.CommitFile, error) {
	switch {
	case onLeft && !onRight:
		return model.CommitFile{Status: "D", Path: path}, nil
	case !onLeft && onRight:
		return model.CommitFile{Status: "A", Path: path}, nil
	case !onLeft && !onRight:
		return model.CommitFile{}, nil
	}
	same, err := s.sameBytes(ctx, left.Endpoint(), right.Endpoint(), path)
	if err != nil {
		return model.CommitFile{}, err
	}
	if same {
		return model.CommitFile{}, nil
	}
	return model.CommitFile{Status: "M", Path: path}, nil
}

// sameBytes reads one path from both endpoints and compares. Both sides are
// read through FileRef/ResolveBytes, which is what lets a frozen shelf tar and
// a live commit sit on either side without this function knowing the
// difference.
func (s *Service) sameBytes(ctx context.Context, left, right model.Endpoint, path string) (bool, error) {
	lb, err := s.ResolveBytes(ctx, left.FileRef(path))
	if err != nil {
		return false, err
	}
	rb, err := s.ResolveBytes(ctx, right.FileRef(path))
	if err != nil {
		return false, err
	}
	return bytes.Equal(lb, rb), nil
}
