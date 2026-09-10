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

// linkDiffSpec is the diff a link's target names. It reuses HunkDiffSpec so a
// commit link means the commit's OWN change (parent → commit, the empty tree
// for a root commit) — the same rule `gg diff --hunks` and `gg note --hunk N`
// follow, which is what makes a hunk number portable between them.
func linkDiffSpec(ctx context.Context, svc *domain.Service, res domain.Resolved) (model.DiffSpec, error) {
	var paths []string
	if res.Addr.Path != "" {
		paths = []string{res.Addr.Path}
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
