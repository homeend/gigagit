package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/model"
)

// PatchLosesSetError is ComparePatchSets' refusal: ComparePatch renders whole
// ENDPOINTS, and at least one side is a file set its endpoint cannot
// reproduce. Left/Right say which, so a frontend can name the side.
type PatchLosesSetError struct{ Left, Right bool }

func (e *PatchLosesSetError) Error() string {
	return "a patch renders whole endpoints, and this comparison names a file set (a link with a /<path>, or an <a>..<b> change-set)"
}

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

// ComparePatchSets is ComparePatch for a caller holding the comparison's FILE
// SETS: it refuses (a *PatchLosesSetError) when either side would be quietly
// widened, and otherwise renders the two endpoints. It lives here — not in a
// frontend — because the rule is a fact about the compare algebra: a frontend
// that called ComparePatch with left.Endpoint(), right.Endpoint() directly
// would print a different comparison from the one its own listing shows.
func (s *Service) ComparePatchSets(ctx context.Context, left, right FileSet) (string, error) {
	if l, r := left.patchLosesKeySet(), right.patchLosesKeySet(); l || r {
		return "", &PatchLosesSetError{Left: l, Right: r}
	}
	return s.ComparePatch(ctx, left.ep, right.ep)
}
