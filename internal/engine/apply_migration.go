package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/repogate"
)

// ApplyMigration discards a store's unreadable data and stamps the new format
// marker. It is the ONLY write preflight ever performs, and it happens only
// after explicit consent — the frontends ask; this op does not.
//
// Refs is computed by the caller (domain) so the op stays a dumb executor: it
// never decides WHAT is stale, only removes what it was handed.
type ApplyMigration struct {
	Feature string   // English feature id, for the summary
	Store   string   // store whose marker is stamped
	To      int      // format written after the migration
	Refs    []string // refs to delete; may be empty
}

var _ Operation = ApplyMigration{}

func (op ApplyMigration) LockMode() repogate.Mode { return repogate.RefWrite }

func (op ApplyMigration) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Store == "" {
		return Result{}, fmt.Errorf("apply migration: Store is required")
	}
	if op.To <= 0 {
		return Result{}, fmt.Errorf("apply migration: To must be a positive format number")
	}

	deps.emit(ctx, Progress{Step: "migrating store", Detail: op.Store})

	for _, ref := range op.Refs {
		if err := deps.Repo.DeleteRef(ctx, ref); err != nil {
			return Result{}, fmt.Errorf("apply migration: deleting %s: %w", ref, err)
		}
	}
	if err := deps.Repo.StampStoreFormat(ctx, op.Store, op.To); err != nil {
		return Result{}, fmt.Errorf("apply migration: stamping %s format %d: %w", op.Store, op.To, err)
	}

	res := Result{Changed: true}.WithSummary("migrated %s to format %d (%d entries discarded)", op.Store, op.To, len(op.Refs))
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
