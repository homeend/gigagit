package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/preview"
)

// PreviewStatePath overrides the previews root dir ("" = XDG default).
var PreviewStatePath string

// PreviewsDisabled turns the surface off process-wide — the TEST seam for
// packages that cannot import internal/preview (tui/web TestMain). An
// injected store (UsePreviewsDir) still wins for that Service.
var PreviewsDisabled bool

// UsePreviewsDir points one Service at its own store under dir. It records
// the DIRECTORY as well as the store: the previews migration works on files
// (previews.toml is a sibling of savedcompare.toml), and a store value cannot
// be asked where it lives.
func (s *Service) UsePreviewsDir(dir string) {
	s.UseSavedCompareDir(dir)
	s.SetPreviewStore(preview.NewFileStore(dir))
}

// SetPreviewStore injects a store (tests); nil re-arms lazy resolution.
func (s *Service) SetPreviewStore(st preview.Store) {
	s.mu.Lock()
	s.preview = st
	s.mu.Unlock()
}

// previewStore resolves (once) the per-repo store, keyed by git common dir
// under <state>/gg/previews. nil = disabled (no state dir, or PreviewsDisabled).
func (s *Service) previewStore(ctx context.Context) preview.Store {
	s.mu.Lock()
	st := s.preview
	s.mu.Unlock()
	if st != nil {
		return st
	}
	// ONE derivation of the directory, shared with the saved-comparison store
	// that supersedes this one — they are the same directory, and the
	// previews migration finds previews.toml there as a sibling. Two
	// derivations of a path that must agree is how a user's data gets
	// orphaned; domain's stateBaseDir caller gate refuses the duplicate
	// outright.
	root := s.savedCompareDir(ctx)
	if root == "" {
		return nil
	}
	fs := preview.NewFileStore(root)
	s.mu.Lock()
	if s.preview == nil {
		s.preview = fs
	}
	st = s.preview
	s.mu.Unlock()
	return st
}
