package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/repogate"
)

// ApplyMigration runs a migration's action and stamps the new format marker.
// It is the ONLY write preflight ever performs, and it happens only after the
// migration's consent rule is satisfied — the frontends ask; this op does not.
//
// Action is constructed by the caller (domain) so the op stays a dumb
// executor: it never decides WHAT is stale, only runs what it was handed,
// under the reservation, and stamps the marker afterwards.
type ApplyMigration struct {
	Feature string          // English feature id, for the summary
	Store   string          // store whose marker is stamped
	To      int             // format written after the migration
	Action  MigrationAction // what to do to the stale data
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
	if op.Action == nil {
		return Result{}, fmt.Errorf("apply migration: Action is required")
	}

	deps.emit(ctx, Progress{Step: "migrating store", Detail: op.Store})

	n, err := op.Action.Apply(ctx, deps)
	if err != nil {
		return Result{}, fmt.Errorf("apply migration: %s: %w", op.Action.Describe(), err)
	}
	// The marker is stamped only after the action SUCCEEDED: a half-migrated
	// store must never be labelled migrated.
	if err := deps.Repo.StampStoreFormat(ctx, op.Store, op.To); err != nil {
		return Result{}, fmt.Errorf("apply migration: stamping %s format %d: %w", op.Store, op.To, err)
	}

	res := Result{Changed: true}.WithSummary("migrated %s to format %d (%d entries)", op.Store, op.To, n)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
