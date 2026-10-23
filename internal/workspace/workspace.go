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
	"strconv"
	"strings"
	"time"
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
	// MaxNameRunes bounds a workspace name.
	MaxNameRunes = 40
)

var (
	// ErrFull is Add's refusal once a workspace holds MaxRepos members.
	ErrFull = errors.New("workspace: already has 5 repositories")
	// ErrDuplicate is Add's refusal for a path that is already a member.
	ErrDuplicate = errors.New("workspace: already a member")
	// ErrNameEmpty, ErrNameLong and ErrNameChar are what ValidName wraps:
	// a frontend matches them to word the refusal in its own language.
	ErrNameEmpty = errors.New("a workspace needs a name")
	ErrNameLong  = errors.New("a workspace name is too long")
	ErrNameChar  = errors.New("a workspace name may not contain that character")
	// ErrNewerFormat: the file was written by a newer gg; an edit by this
	// one would lose what it does not understand, so it is refused.
	ErrNewerFormat = errors.New("workspace: the registry was written by a newer gg")
	// ErrNoSuchWorkspace: an entry-level edit named a workspace the file no
	// longer holds (another gg deleted or renamed it); nothing is written.
	ErrNoSuchWorkspace = errors.New("workspace: no such workspace")
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
// MaxNameRunes, or holding '/', '?' (a gg:// link separator) or a control
// character.
func ValidName(name string) error {
	if strings.TrimSpace(name) == "" {
		return ErrNameEmpty
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return fmt.Errorf("%w: at most %d characters", ErrNameLong, MaxNameRunes)
	}
	for _, r := range name {
		if r == '/' || r == '?' || unicode.IsControl(r) {
			return fmt.Errorf("%w: %q", ErrNameChar, r)
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
	return writeLocked(path, ws)
}

// Update applies mutate to the registry AS IT IS at that moment — the
// file re-read under the lock, mutated, written back — and returns the
// written list. Two processes editing at once thus serialize on the lock
// instead of the last whole-file write winning.
func Update(path string, mutate func([]Entry) []Entry) ([]Entry, error) {
	if path == "" {
		return nil, errors.New("workspace: no state path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	unlock, err := filelock.Acquire(path + ".lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	cur, err := loadLocked(path)
	if err != nil {
		return nil, err
	}
	ws := mutate(cur)
	if err := writeLocked(path, ws); err != nil {
		return nil, err
	}
	return ws, nil
}

// loadLocked is Update's read: a missing file is an empty registry; a
// newer gg's file refuses the edit (ErrNewerFormat); a corrupt file is
// moved aside (<path>.corrupt-<unix>) so a hand edit's typo never costs
// the workspaces it held, and the edit starts a fresh registry.
func loadLocked(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f file
	if err := toml.Unmarshal(data, &f); err != nil {
		aside := path + ".corrupt-" + strconv.FormatInt(time.Now().Unix(), 10)
		for n := 2; ; n++ { // a second corrupt file in the same second keeps the first
			if _, serr := os.Lstat(aside); os.IsNotExist(serr) {
				break
			}
			aside = path + ".corrupt-" + strconv.FormatInt(time.Now().Unix(), 10) + "-" + strconv.Itoa(n)
		}
		if rerr := os.Rename(path, aside); rerr != nil {
			return nil, fmt.Errorf("workspace: corrupt registry could not be moved aside: %w", rerr)
		}
		return nil, nil
	}
	if f.Format > Format {
		return nil, ErrNewerFormat
	}
	return f.Workspaces, nil
}

// Edit applies change to the workspace named name in the file as it is
// now (Update's rule); the other workspaces and the entry's fields the
// change does not touch are kept. A name the file no longer holds (another
// gg deleted or renamed it) is ErrNoSuchWorkspace with the list as it is:
// a stale popup must not bring a workspace back.
func Edit(path, name string, change func(*Entry)) ([]Entry, error) {
	found := false
	ws, err := Update(path, func(ws []Entry) []Entry {
		for i := range ws {
			if strings.EqualFold(ws[i].Name, name) {
				found = true
				change(&ws[i])
				return ws
			}
		}
		return ws
	})
	if err == nil && !found {
		return ws, ErrNoSuchWorkspace
	}
	return ws, err
}

// Normalized is e with one entry per repository (the first spelling of
// each checkout key wins) and at most MaxRepos members; a SettingsRepo no
// longer among them is cleared. The popup keeps a file in this shape; a
// hand edit may not.
func (e Entry) Normalized() Entry {
	out := Entry{Name: e.Name, SettingsRepo: e.SettingsRepo}
	seen := map[model.CheckoutKey]bool{}
	for _, p := range e.Repos {
		k := model.KeyOf(p)
		if seen[k] || len(out.Repos) >= MaxRepos {
			continue
		}
		seen[k] = true
		out.Repos = append(out.Repos, p)
	}
	if out.SettingsRepo != "" && !out.Has(out.SettingsRepo) {
		out.SettingsRepo = ""
	}
	return out
}

// IndexOf is the position of path among the members (by checkout key),
// -1 when it is not one.
func (e Entry) IndexOf(path string) int {
	key := model.KeyOf(path)
	for i, p := range e.Repos {
		if model.KeyOf(p) == key {
			return i
		}
	}
	return -1
}

// writeLocked is Save's write, the lock held by the caller.
func writeLocked(path string, ws []Entry) error {
	dir := filepath.Dir(path)
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

// Rename gives the workspace named old the name new (case-insensitively,
// in place); an old that is gone renames nothing.
func Rename(ws []Entry, old, new string) []Entry {
	for i := range ws {
		if strings.EqualFold(ws[i].Name, old) {
			ws[i].Name = new
			return ws
		}
	}
	return ws
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
