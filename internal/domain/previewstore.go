package domain

import (
	"github.com/homeend/gigagit/internal/savedcompare"
)

// The merge-preview SEAMS. The store itself is internal/savedcompare (a
// merge preview is a saved comparison whose right half is absent, spec §4.5);
// what remains here are the names internal/tui and internal/web already use
// in their TestMain, which cannot import a store package.
//
// They keep their "preview" spelling deliberately: renaming them is churn
// that belongs with plan 3c, when the Previews tab becomes the
// saved-comparisons tab and the vocabulary changes for the user too.

// PreviewStatePath overrides the state root process-wide ("" = XDG default).
var PreviewStatePath string

// PreviewsDisabled turns the surface off process-wide — the TEST seam for
// packages that cannot import the store (tui/web TestMain). An injected store
// (UsePreviewsDir) still wins for that Service.
var PreviewsDisabled bool

// UsePreviewsDir points one Service at its own store under dir. It records
// the DIRECTORY as well as the store: the previews migration works on files
// (previews.toml is a sibling of savedcompare.toml), and a store value cannot
// be asked where it lives.
func (s *Service) UsePreviewsDir(dir string) {
	// Order matters: UseSavedCompareDir re-arms lazy resolution, so the
	// explicit store goes in AFTER it, never before.
	s.UseSavedCompareDir(dir)
	s.SetSavedCompareStore(savedcompare.NewFileStore(dir))
}

// SetPreviewStore injects a store (tests); nil re-arms lazy resolution.
//
// Deprecated: the preview store and the saved-comparison store are one store.
// Call SetSavedCompareStore.
func (s *Service) SetPreviewStore(st savedcompare.Store) { s.SetSavedCompareStore(st) }
