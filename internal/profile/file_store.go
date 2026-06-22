package profile

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/gigagit/gg/internal/model"
)

// FileStore keeps an atomic-rewrite TOML registry under root/profiles.toml,
// all rows belonging to one scope.
type FileStore struct {
	root  string
	scope model.ProfileScope
}

// NewFileStore roots a store at a scope's directory (caller-supplied).
func NewFileStore(root string, scope model.ProfileScope) *FileStore {
	return &FileStore{root: root, scope: scope}
}

type index struct {
	Profiles []model.Profile `toml:"profiles"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "profiles.toml") }

func (fs *FileStore) read() index {
	var idx index
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return idx
	}
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}
	}
	for i := range idx.Profiles {
		idx.Profiles[i].Scope = fs.scope // Scope is toml:"-"; set from the store
	}
	return idx
}

// write persists idx via temp-file + rename (the seq-state pattern).
func (fs *FileStore) write(idx index) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "profiles-*.toml")
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
	if err := os.Rename(name, fs.path()); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

var slugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// ProfileID derives a stable id from a profile name (slug). Same name → same
// id, so Add is idempotent and rename is remove-then-add.
func ProfileID(name string) string {
	return strings.Trim(strings.ToLower(slugRe.ReplaceAllString(name, "-")), "-")
}

func (fs *FileStore) Add(p model.Profile) (model.Profile, error) {
	p.ID = ProfileID(p.Name)
	p.Scope = fs.scope
	if p.Created.IsZero() {
		p.Created = time.Now()
	}
	idx := fs.read()
	for i := range idx.Profiles {
		if idx.Profiles[i].ID == p.ID { // same slug → idempotent replace
			idx.Profiles[i] = p
			return p, fs.write(idx)
		}
	}
	idx.Profiles = append(idx.Profiles, p)
	return p, fs.write(idx)
}

func (fs *FileStore) Get(id string) (model.Profile, error) {
	for _, p := range fs.read().Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return model.Profile{}, ErrNotFound
}

func (fs *FileStore) List() ([]model.Profile, error) {
	ps := fs.read().Profiles
	sort.SliceStable(ps, func(a, b int) bool { return ps[a].Created.After(ps[b].Created) })
	return ps, nil
}

func (fs *FileStore) Remove(id string) error {
	idx := fs.read()
	kept := idx.Profiles[:0]
	found := false
	for _, p := range idx.Profiles {
		if p.ID == id {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return ErrNotFound
	}
	idx.Profiles = kept
	return fs.write(idx)
}

var _ Store = (*FileStore)(nil)
