package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/savedcompare"
)

// convertPreviews folds the legacy merge-preview store into savedcompare.
// Lossless: ids, labels and creation times are carried across, and preview
// notes need no migration because they key on the branch NAMES.
//
// It lives in domain rather than engine although engine declares the
// interface: domain owns every store in this project, and an engine operation
// reaching into a machine-local TOML file would be the first exception. The
// interface is satisfied from any package, so the convention costs nothing.
type convertPreviews struct {
	Dir  string         // the per-repo state directory holding both files
	Repo model.LinkRepo // this repository's link identity
}

var _ engine.MigrationAction = convertPreviews{}

func (a convertPreviews) Describe() string { return "converting saved merge previews" }

func (a convertPreviews) Apply(ctx context.Context, deps engine.OpDeps) (int, error) {
	return savedcompare.ConvertLegacy(a.Dir, a.Repo)
}
