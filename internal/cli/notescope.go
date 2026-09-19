package cli

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/domain"
)

// noteScopeFromLink is previewTargetFromLink for BOTH bounded link kinds: a
// merge-preview link (the resolver already built its set) or a change-set
// link @<a>..<b>, whose set is built here. Every note verb and the highlight
// verb take their scope from this one adapter, so a pair link runs the very
// lane --preview <a>..<b> runs.
//
// domain.Resolved.Preview stays nil for a pair ON PURPOSE: linknav and both
// steer consumers dispatch on it and refuse a preview with no branch names,
// so filling it would break `gg open <pair link>`, which rides State "pair".
func noteScopeFromLink(ctx context.Context, svc *domain.Service, res domain.Resolved) (previewTarget, bool, error) {
	if tgt, ok := previewTargetFromLink(res); ok {
		return tgt, true, nil
	}
	p := res.Pair
	if p == nil {
		return previewTarget{}, false, nil
	}
	set, err := svc.PairNotes(ctx, p.A, p.B)
	if err != nil {
		return previewTarget{}, false, err
	}
	if !set.OK() {
		// Unreachable through resolveLinkArg (the resolver already proved both
		// halves), kept so a caller with a hand-built Resolved fails loudly.
		return previewTarget{}, false, fmt.Errorf("preview: missing commit in %s..%s", p.A, p.B)
	}
	return previewTarget{Set: set, Spec: set.DiffSpec()}, true, nil
}
