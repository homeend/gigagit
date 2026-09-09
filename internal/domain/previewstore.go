package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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
		base := previewBaseDir()
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

// previewBaseDir mirrors notesBaseDir with a "previews" leaf.
func previewBaseDir() string {
	// An explicitly-set $XDG_STATE_HOME wins on every platform (it is a
	// deliberate override — and the only way tests can isolate state on
	// Windows); %LocalAppData% is the ambient Windows default.
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "previews")
	}
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "previews")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "previews")
}
