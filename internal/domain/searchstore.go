package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gigagit/gg/internal/searchhist"
)

// SearchStatePath overrides the search-history root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var SearchStatePath string

// SetSearchStore injects a store (tests).
func (s *Service) SetSearchStore(st searchhist.Store) {
	s.mu.Lock()
	s.searchhist = st
	s.mu.Unlock()
}

// searchStore resolves (once) the per-repo history store, keyed by git common
// dir under the XDG state dir. Returns nil (history disabled) when no state dir
// is resolvable — mirroring the shelf/bookmark posture.
func (s *Service) searchStore(ctx context.Context) searchhist.Store {
	s.mu.Lock()
	if s.searchhist != nil {
		st := s.searchhist
		s.mu.Unlock()
		return st
	}
	s.mu.Unlock()

	root := SearchStatePath
	if root == "" {
		base := searchBaseDir()
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
		}
		root = filepath.Join(base, key)
	}
	st := searchhist.NewFileStore(root)
	s.mu.Lock()
	s.searchhist = st
	s.mu.Unlock()
	return st
}

// searchBaseDir resolves <state>/gg/search cross-platform (mirrors shelfBaseDir).
// "" when no home/state dir exists.
func searchBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "search")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "search")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "search")
}

// RecordSearch appends an Enter-confirmed phrase to scope's ring. Best-effort:
// a nil store (history disabled) or a write error is silently ignored, like the
// other side stores. rawSize is the unclamped config value.
func (s *Service) RecordSearch(ctx context.Context, scope, phrase string, rawSize int) {
	st := s.searchStore(ctx)
	if st == nil {
		return
	}
	_ = st.Record(scope, phrase, EffectiveSearchHistorySize(rawSize))
}

// SearchHistoryAll returns every ring (newest-first), or an empty map when
// history is disabled.
func (s *Service) SearchHistoryAll(ctx context.Context) map[string][]string {
	st := s.searchStore(ctx)
	if st == nil {
		return map[string][]string{}
	}
	return st.All()
}
