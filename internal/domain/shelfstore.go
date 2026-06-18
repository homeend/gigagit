package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gigagit/gg/internal/shelf"
)

// ShelfStatePath overrides the shelf root directory. "" uses the default XDG
// location. cmd/gg leaves it ""; tests point it at a temp dir.
var ShelfStatePath string

// SetShelfStore injects a store (tests).
func (s *Service) SetShelfStore(st shelf.Store) {
	s.mu.Lock()
	s.shelf = st
	s.mu.Unlock()
}

// shelfStore resolves (once) the per-repo content store, keyed by git common
// dir under the XDG state dir. Returns nil (shelf disabled) when no state dir
// is resolvable — mirroring repos.toml's best-effort posture.
func (s *Service) shelfStore(ctx context.Context) shelf.Store {
	s.mu.Lock()
	if s.shelf != nil {
		st := s.shelf
		s.mu.Unlock()
		return st
	}
	s.mu.Unlock()

	root := ShelfStatePath
	if root == "" {
		base := shelfBaseDir() // <state>/gg/shelf
		if base == "" {
			return nil
		}
		key := "unknown"
		if cd, err := s.GitCommonDir(ctx); err == nil {
			key = repoKey(strings.TrimSpace(cd))
		}
		root = filepath.Join(base, key)
	}
	st := shelf.NewFileStore(root)
	s.mu.Lock()
	s.shelf = st
	s.mu.Unlock()
	return st
}

// shelfBaseDir resolves <state>/gg/shelf cross-platform (mirrors
// repos.DefaultStatePath). "" when no home/state dir exists.
func shelfBaseDir() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "shelf")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "shelf")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "shelf")
}

// repoKey hashes the git common dir to a short stable directory name.
func repoKey(commonDir string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(commonDir)))
	return hex.EncodeToString(sum[:8])
}
