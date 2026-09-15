package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// linkExit maps a link failure onto an exit code: 2 for a malformed link
// (a caller mistake, like every other usage error), 1 for a link that is
// well-formed but names nothing resolvable here.
func linkExit(verb string, err error, stderr io.Writer) int {
	fmt.Fprintf(stderr, "%s: %v\n", verb, err)
	if errors.Is(err, model.ErrLink) {
		return 2
	}
	return 1
}

// openLinkTarget opens the checkout a resolved link named, with the SAME
// per-service setup Run gives the cwd's service. Opening it bare (plain
// domain.Open) skipped the EOL-only filter and the versions policy, so
// `gg diff <link>` could disagree with `gg diff` run inside that checkout.
func openLinkTarget(res domain.Resolved) *domain.Service {
	svc := domain.Open(res.Checkout)
	setupCLIService(svc)
	return svc
}

// linkDiffSpec is the diff a link's target names. It reuses HunkDiffSpec so a
// commit link means the commit's OWN change (parent → commit, the empty tree
// for a root commit) — the same rule `gg diff --hunks` and `gg note --hunk N`
// follow, which is what makes a hunk number portable between them.
func linkDiffSpec(ctx context.Context, svc *domain.Service, res domain.Resolved) (model.DiffSpec, error) {
	var paths []string
	if res.Addr.Path != "" {
		paths = []string{res.Addr.Path}
	}
	// A PREVIEW link's patch is merge-base → source tip, never the tip commit's
	// own parent→tip change: `gg diff <preview link> --hunks` and
	// `gg note add <preview link>#N` must number the same hunks as
	// `gg diff --preview … --hunks` (ruling 1 of feature A, ruling 5 here).
	if tgt, ok := previewTargetFromLink(res); ok {
		return tgt.withPaths(paths), nil
	}
	switch res.Addr.State {
	case model.StateStaged:
		return svc.HunkDiffSpec(ctx, true, "", paths)
	case model.StateCommitted:
		return svc.HunkDiffSpec(ctx, false, res.Addr.Commit, paths)
	default:
		return svc.HunkDiffSpec(ctx, false, "", paths)
	}
}
