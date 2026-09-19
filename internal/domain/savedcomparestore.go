package domain

import (
	"context"
	"path/filepath"
	"strings"
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
func (s *Service) UseSavedCompareDir(dir string) {
	s.mu.Lock()
	s.savedCompareRoot = dir
	s.mu.Unlock()
}
