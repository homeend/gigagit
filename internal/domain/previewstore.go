package domain

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/preview"
)

// PreviewStatePath overrides the previews root dir ("" = XDG default).
var PreviewStatePath string

// PreviewsDisabled turns the surface off process-wide — the TEST seam for
// packages that cannot import internal/preview (tui/web TestMain). An
// injected store (UsePreviewsDir) still wins for that Service.
var PreviewsDisabled bool

// UsePreviewsDir points one Service at its own store under dir.
func (s *Service) UsePreviewsDir(dir string) { s.SetPreviewStore(preview.NewFileStore(dir)) }

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
	if PreviewsDisabled {
		return nil
	}
	root := PreviewStatePath
	if root == "" {
		base := stateBaseDir("previews")
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd))
		}
		root = filepath.Join(base, key)
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
