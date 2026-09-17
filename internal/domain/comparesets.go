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
		return s.CompareFiles(ctx, left.Endpoint(), right.Endpoint())

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
		if !inLeft[p] && right.Has(p) {
			out = append(out, model.CommitFile{Status: "A", Path: p})
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
