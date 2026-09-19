package engine

import (
	"context"
	"fmt"
)

// MigrationAction is the BODY of a migration: what to do to the stale data
// before the new format marker is stamped.
//
// It exists because gg's stores are not all the same kind of thing. The first
// migration deleted git refs, so ApplyMigration deleted git refs; the next
// one converts a machine-local TOML file, which is not a ref and not a
// deletion. Rather than grow a field per store, the op takes the action it is
// to run — the caller (domain) still decides WHAT, and the op still only runs
// it under the reservation.
type MigrationAction interface {
	// Describe names the action in English for the operation summary.
	Describe() string
	// Apply performs the migration and reports how many entries it affected.
	Apply(ctx context.Context, deps OpDeps) (int, error)
}

// DiscardRefs deletes the listed refs. This is ApplyMigration's original
// hardcoded body, extracted unchanged: the branch-versions migration (format
// 1 → 2) is its caller, and a format-1 version ref can never be converted
// because it records no merge base.
type DiscardRefs struct {
	Refs []string
}

func (a DiscardRefs) Describe() string { return "discarding stale refs" }

func (a DiscardRefs) Apply(ctx context.Context, deps OpDeps) (int, error) {
	for _, ref := range a.Refs {
		if err := deps.Repo.DeleteRef(ctx, ref); err != nil {
			return 0, fmt.Errorf("deleting %s: %w", ref, err)
		}
	}
	return len(a.Refs), nil
}
