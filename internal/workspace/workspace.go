// Package workspace is gigagit's machine-local registry of WORKSPACES: named,
// ordered sets of up to MaxRepos repositories the TUI opens together (spec
// docs/superpowers/specs/2026-10-22-workspaces-design.md). It is state, not
// config: workspaces.toml lives beside repos.toml under the user's state dir
// and is never committed. Records only — the package knows nothing about
// git; a member is identified by its main checkout path, compared with
// model.KeyOf. DAG leaf: stdlib + go-toml + model + filelock.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
)

const (
	// MaxRepos is the member cap of one workspace.
	MaxRepos = 5
	// Format is the on-disk format this gg writes; a newer file is left alone.
	Format = 1
	// maxNameRunes bounds a workspace name.
	maxNameRunes = 40
)

var (
	// ErrFull is Add's refusal once a workspace holds MaxRepos members.
	ErrFull = errors.New("workspace: already has 5 repositories")
	// ErrDuplicate is Add's refusal for a path that is already a member.
	ErrDuplicate = errors.New("workspace: already a member")
)

// Entry is one workspace.
type Entry struct {
	Name  string   `toml:"name"`
	Repos []string `toml:"repos"` // absolute main-checkout paths, in walk order (≤ MaxRepos)
	// SettingsRepo names the member whose process-wide config (theme, [ui],
	// …) the workspace uses; "" = the repository gg was opened in.
	SettingsRepo string `toml:"settings_repo,omitempty"`
}

// file is the on-disk shape of workspaces.toml.
type file struct {
	Format     int     `toml:"format"`
	Workspaces []Entry `toml:"workspace"`
}

// ValidName reports why name is not a workspace name: empty or blank, over
// maxNameRunes, or holding '/', '?' (a gg:// link separator) or a control
// character.
func ValidName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("a workspace needs a name")
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return fmt.Errorf("a workspace name has at most %d characters", maxNameRunes)
	}
	for _, r := range name {
		if r == '/' || r == '?' || unicode.IsControl(r) {
			return fmt.Errorf("a workspace name may not contain %q", r)
		}
	}
	return nil
}

// Load reads the registry at path. A missing, unreadable or corrupt file,
// an empty path, or a file written by a newer gg (Format above ours) all
// read as nil: the registry is best-effort state and must never block gg.
func Load(path string) []Entry {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f file
	if err := toml.Unmarshal(data, &f); err != nil {
		return nil
	}
	if f.Format > Format {
		return nil
	}
	return f.Workspaces
}

// Save writes ws to path atomically (temp + rename in path's directory)
// under the shared cross-process file lock (<path>.lock), creating parent
// directories as needed. A nil ws writes an empty registry.
func Save(path string, ws []Entry) error {
	if path == "" {
		return errors.New("workspace: no state path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	unlock, err := filelock.Acquire(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	data, err := toml.Marshal(file{Format: Format, Workspaces: ws})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "workspaces-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Containing is the workspaces key belongs to, in registry order; nil when
// none.
func Containing(ws []Entry, key model.CheckoutKey) []Entry {
	var out []Entry
	for _, e := range ws {
		for _, p := range e.Repos {
			if model.KeyOf(p) == key {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// Has reports whether path is a member (by checkout key).
func (e Entry) Has(path string) bool {
	key := model.KeyOf(path)
	for _, p := range e.Repos {
		if model.KeyOf(p) == key {
			return true
		}
	}
	return false
}

// Add appends path as the last member: ErrDuplicate when it is one already
// (checked first — a duplicate is never "full"), ErrFull at MaxRepos.
func (e *Entry) Add(path string) error {
	if e.Has(path) {
		return ErrDuplicate
	}
	if len(e.Repos) >= MaxRepos {
		return ErrFull
	}
	e.Repos = append(e.Repos, path)
	return nil
}

// Remove drops path from the members (by checkout key); a SettingsRepo that
// named it is cleared. A non-member is a no-op.
func (e *Entry) Remove(path string) {
	key := model.KeyOf(path)
	kept := e.Repos[:0]
	for _, p := range e.Repos {
		if model.KeyOf(p) != key {
			kept = append(kept, p)
		}
	}
	e.Repos = kept
	if e.SettingsRepo != "" && model.KeyOf(e.SettingsRepo) == key {
		e.SettingsRepo = ""
	}
}

// Move shifts member i by delta positions, clamped to the list; an index
// out of range is a no-op.
func (e *Entry) Move(i, delta int) {
	n := len(e.Repos)
	if i < 0 || i >= n {
		return
	}
	j := max(0, min(n-1, i+delta))
	if j == i {
		return
	}
	p := e.Repos[i]
	if j > i {
		copy(e.Repos[i:j], e.Repos[i+1:j+1])
	} else {
		copy(e.Repos[j+1:i+1], e.Repos[j:i])
	}
	e.Repos[j] = p
}

// Find is the workspace named name (case-insensitively).
func Find(ws []Entry, name string) (Entry, bool) {
	for _, e := range ws {
		if strings.EqualFold(e.Name, name) {
			return e, true
		}
	}
	return Entry{}, false
}

// Upsert replaces the workspace of e's name (case-insensitively, keeping
// its position) or appends e.
func Upsert(ws []Entry, e Entry) []Entry {
	for i := range ws {
		if strings.EqualFold(ws[i].Name, e.Name) {
			ws[i] = e
			return ws
		}
	}
	return append(ws, e)
}

// Delete drops the workspace named name (case-insensitively).
func Delete(ws []Entry, name string) []Entry {
	out := ws[:0]
	for _, e := range ws {
		if !strings.EqualFold(e.Name, name) {
			out = append(out, e)
		}
	}
	return out
}
