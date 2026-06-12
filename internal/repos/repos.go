// Package repos maintains gigagit's machine-local registry of recently opened
// repositories — the data behind the repo switcher. The registry is state, not
// config: it lives under the user's state dir and is never committed.
package repos

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Entry is one known repository.
type Entry struct {
	Path       string    `toml:"path"`        // absolute top-level path
	LastOpened time.Time `toml:"last_opened"` // MRU sort key
}

// Name is an Entry's display name.
func Name(e Entry) string { return filepath.Base(e.Path) }

// registry is the on-disk shape of repos.toml.
type registry struct {
	Repos []Entry `toml:"repos"`
}

// DefaultStatePath resolves the platform-appropriate registry location:
// %LocalAppData%/gg/repos.toml on Windows, else $XDG_STATE_HOME/gg/repos.toml,
// else ~/.local/state/gg/repos.toml. "" (recording disabled) if no home exists.
func DefaultStatePath() string {
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LocalAppData"); lad != "" {
			return filepath.Join(lad, "gg", "repos.toml")
		}
	}
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "gg", "repos.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "gg", "repos.toml")
}

// read loads the raw registry. A missing or corrupt file acts as empty — the
// registry is best-effort history and must never block gg.
func read(statePath string) registry {
	var reg registry
	data, err := os.ReadFile(statePath)
	if err != nil {
		return reg
	}
	if err := toml.Unmarshal(data, &reg); err != nil {
		return registry{}
	}
	return reg
}

// alive reports whether an entry's path still exists.
func alive(e Entry) bool {
	_, err := os.Stat(e.Path)
	return err == nil
}

// prune drops entries whose path no longer exists.
func prune(reg registry) registry {
	kept := reg.Repos[:0]
	for _, e := range reg.Repos {
		if alive(e) {
			kept = append(kept, e)
		}
	}
	reg.Repos = kept
	return reg
}

// Load returns the known repos MRU-first (most recently opened first), with
// dead paths dropped. An empty statePath, or a missing/corrupt file, yields an
// empty list. Pruning here is in-memory only; the file is rewritten on the
// next Touch/Remove.
func Load(statePath string) []Entry {
	if statePath == "" {
		return nil
	}
	reg := prune(read(statePath))
	sort.SliceStable(reg.Repos, func(a, b int) bool {
		return reg.Repos[a].LastOpened.After(reg.Repos[b].LastOpened)
	})
	return reg.Repos
}

// Touch records repoPath with now as its LastOpened, deduplicating by cleaned
// absolute path and pruning dead entries, then persists atomically. An empty
// statePath disables recording (a silent no-op) — production entry points wire
// the real path; tests and bare constructors stay hermetic.
func Touch(statePath, repoPath string, now time.Time) error {
	if statePath == "" {
		return nil
	}
	if abs, err := filepath.Abs(repoPath); err == nil {
		repoPath = abs
	}
	repoPath = filepath.Clean(repoPath)

	reg := prune(read(statePath))
	found := false
	for i := range reg.Repos {
		if reg.Repos[i].Path == repoPath {
			reg.Repos[i].LastOpened = now
			found = true
			break
		}
	}
	if !found {
		reg.Repos = append(reg.Repos, Entry{Path: repoPath, LastOpened: now})
	}
	return write(statePath, reg)
}

// Remove forgets repoPath. Removing an absent entry is not an error. The repo
// on disk is never touched — only the registry entry.
func Remove(statePath, repoPath string) error {
	if statePath == "" {
		return nil
	}
	repoPath = filepath.Clean(repoPath)
	reg := read(statePath)
	kept := reg.Repos[:0]
	for _, e := range reg.Repos {
		if e.Path != repoPath {
			kept = append(kept, e)
		}
	}
	reg.Repos = kept
	return write(statePath, reg)
}

// write persists reg via a temp file + rename in the target directory, so a
// concurrent reader never sees a half-written file (the seq-state pattern).
func write(statePath string, reg registry) error {
	dir := filepath.Dir(statePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(reg)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "repos-*.toml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, statePath); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
