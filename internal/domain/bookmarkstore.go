package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/homeend/gigagit/internal/bookmark"
)

// BookmarkStatePath overrides the bookmark root dir. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var BookmarkStatePath string

// SetBookmarkStore injects a store (tests).
func (s *Service) SetBookmarkStore(st bookmark.Store) {
	s.mu.Lock()
	s.bookmark = st
	s.mu.Unlock()
}

// bookmarkStore resolves (once) the per-repo record store, keyed by git common
// dir under the XDG state dir. Returns nil (bookmarks disabled) when no state
// dir is resolvable — mirroring the shelf's posture.
func (s *Service) bookmarkStore(ctx context.Context) bookmark.Store {
	s.mu.Lock()
	if s.bookmark != nil {
		st := s.bookmark
		s.mu.Unlock()
		return st
	}
	s.mu.Unlock()

	root := BookmarkStatePath
	if root == "" {
		base := bookmarkBaseDir()
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd)) // reuse shelfstore.go's repoKey
		}
		root = filepath.Join(base, key)
	}
	st := bookmark.NewFileStore(root)
	s.mu.Lock()
	s.bookmark = st
	s.mu.Unlock()
	return st
}

// bookmarkBaseDir resolves <state>/gg/bookmark cross-platform (mirrors
// shelfBaseDir). "" when no home/state dir exists.
func bookmarkBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "bookmark")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "bookmark")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "bookmark")
}
