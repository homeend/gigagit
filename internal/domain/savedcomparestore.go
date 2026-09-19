package domain

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/savedcompare"
)

// savedCompareDir is the per-repo state directory for saved comparisons,
// keyed by git common dir under <state>/gg/previews. "" = disabled (no state
// dir, or PreviewsDisabled).
//
// The kind string stays "previews" ON PURPOSE. The legacy previews.toml this
// store absorbs is a sibling in this very directory, so the conversion needs
// no path plumbing and no knowledge of where an older gg used to write; and
// domain's stateBaseDir caller gate keeps its pinned set of kinds unchanged.
// A kind string is a DIRECTORY ON A USER'S DISK: adding one here would buy a
// tidier name at the cost of orphaning the data it is meant to find.
func (s *Service) savedCompareDir(ctx context.Context) string {
	s.mu.Lock()
	root := s.savedCompareRoot
	s.mu.Unlock()
	if root != "" {
		return root
	}
	if PreviewStatePath != "" {
		return PreviewStatePath
	}
	if PreviewsDisabled {
		return ""
	}
	base := stateBaseDir("previews")
	if base == "" {
		return ""
	}
	key := "unknown"
	if cd, err := s.GitCommonDir(ctx); err == nil {
		key = repoKey(strings.TrimSpace(cd))
	}
	return filepath.Join(base, key)
}

// UseSavedCompareDir points ONE Service at its own state directory, so a test
// need not mutate the process-wide PreviewStatePath and can stay parallel.
//
// It RE-ARMS the lazily resolved store. A directory that changed while an
// already-resolved store kept pointing at the old one is not a test artifact:
// it is a read and a write disagreeing about where a user's data lives, which
// is exactly how the mid-plan window made every saved preview go dark.
func (s *Service) UseSavedCompareDir(dir string) {
	s.mu.Lock()
	s.savedCompareRoot = dir
	s.savedCompare = nil
	s.mu.Unlock()
}

// SetSavedCompareStore injects a store (tests); nil re-arms lazy resolution.
func (s *Service) SetSavedCompareStore(st savedcompare.Store) {
	s.mu.Lock()
	s.savedCompare = st
	s.mu.Unlock()
}

// savedCompareStore resolves (once) the per-repo store. nil = disabled (no
// state dir, or PreviewsDisabled).
func (s *Service) savedCompareStore(ctx context.Context) savedcompare.Store {
	s.mu.Lock()
	st := s.savedCompare
	s.mu.Unlock()
	if st != nil {
		return st
	}
	root := s.savedCompareDir(ctx)
	if root == "" {
		return nil
	}
	fs := savedcompare.NewFileStore(root)
	s.mu.Lock()
	if s.savedCompare == nil {
		s.savedCompare = fs
	}
	st = s.savedCompare
	s.mu.Unlock()
	return st
}
