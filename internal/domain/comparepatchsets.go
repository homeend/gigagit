package domain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// patchLosesKeySet reports whether ONE side's key set survives the trip
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
//     why FileSet.narrowed has to exist: bounded-ness and kind cannot see it.
//   - Any other BOUNDED set — above all a PAIR, whose endpoint is commit *b*
//     and not the pair (EvalEndpoint's Pair arm), so re-deriving yields b's
//     whole tree. livePairSpec's lane does not consult the sets at all, and
//     the shelf lane re-derives from the wrong thing; either way the key set
//     is gone.
//
// Asking per pair is what left `pair × shelf` open: "a shelf is on one side,
// so ComparePatch re-derives both sets" is true of the SHELF side only. The
// change-set's members vanished from the patch while a file it never named
// was rendered — the silent wrong answer this guard exists to stop.
func (f FileSet) patchLosesKeySet() bool {
	return f.narrowed || (f.bounded && f.ep.Kind() != model.EndpointShelf)
}

// ComparePatchSets renders a comparison of two FILE SETS as a unified diff —
// the patch of exactly the rows CompareSets lists for them. It lives here, not
// in a frontend, because which lane may render a comparison is a fact about
// the compare algebra.
//
//   - Two sides their endpoints reproduce go to ComparePatch: one git
//     invocation for a live pair, the per-member lane for a shelf entry.
//   - A side that would LOSE its key set there (patchLosesKeySet), and a pair
//     ComparePatch cannot spell (a reversed live pair — model.DiffSpec has no
//     -R), render per member instead. That lane costs one `diff --no-index`
//     and up to two blob reads per row, so it runs only where the fast lane
//     would describe a different comparison, or none.
func (s *Service) ComparePatchSets(ctx context.Context, left, right FileSet) (string, error) {
	if !left.patchLosesKeySet() && !right.patchLosesKeySet() {
		patch, err := s.ComparePatch(ctx, left.ep, right.ep)
		if !errors.Is(err, ErrComparePatchPair) {
			return patch, err
		}
	}
	return s.patchPerMember(ctx, left, right)
}

// patchPerMember renders left → right one listed file at a time: both sides'
// bytes into temp files, `git diff --no-index`, headers relabelled to
// a/<path> b/<path>. It needs nothing of git but bytes, so it renders ANY
// comparison CompareSets can list.
//
// A member's bytes come from Source(path), never from the set's endpoint (a
// `-u` stash keeps untracked files on a third parent), and a rename's left
// side lives at its OLD path. A read error on a side the row says IS present
// propagates; only the side an "A"/"D" status says is absent goes unread.
func (s *Service) patchPerMember(ctx context.Context, left, right FileSet) (string, error) {
	files, err := s.CompareSets(ctx, left, right)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "gg-compare-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	var b strings.Builder
	for i, f := range files {
		lpath := f.Path
		if f.OldPath != "" {
			lpath = f.OldPath
		}
		var lb, rb []byte
		switch f.Status {
		case "A": // left genuinely absent; only the right side is read
			rb, err = s.ResolveBytes(ctx, right.Source(f.Path).FileRef(f.Path))
		case "D": // right genuinely absent; only the left side is read
			lb, err = s.ResolveBytes(ctx, left.Source(lpath).FileRef(lpath))
		default: // both sides are present — resolve both, any error propagates
			lb, err = s.ResolveBytes(ctx, left.Source(lpath).FileRef(lpath))
			if err == nil {
				rb, err = s.ResolveBytes(ctx, right.Source(f.Path).FileRef(f.Path))
			}
		}
		if err != nil {
			return "", err
		}
		if isBinaryContent(lb) || isBinaryContent(rb) {
			// git diff --no-index would print the temp paths on this line
			// (no @@ hunk to flip RelabelNoIndexDiff's header latch), so a
			// binary pair is rendered directly instead of ever being diffed.
			fmt.Fprintf(&b, "Binary files a/%s and b/%s differ\n", lpath, f.Path)
			continue
		}
		lp := filepath.Join(tmp, fmt.Sprintf("l%d", i))
		rp := filepath.Join(tmp, fmt.Sprintf("r%d", i))
		if err := os.WriteFile(lp, lb, 0o600); err != nil {
			return "", err
		}
		if err := os.WriteFile(rp, rb, 0o600); err != nil {
			return "", err
		}
		diff, err := s.DiffNoIndex(ctx, lp, rp)
		if err != nil {
			return "", err
		}
		b.WriteString(RelabelNoIndexDiff(diff, "a/"+lpath, "b/"+f.Path))
	}
	return b.String(), nil
}
