package prefix

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

// FileStore keeps an atomic-rewrite TOML registry under root/prefixes.toml,
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
	Prefixes []model.Prefix `toml:"prefixes"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "prefixes.toml") }

func (fs *FileStore) read() index {
	var idx index
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return idx
	}
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}
	}
	for i := range idx.Prefixes {
		idx.Prefixes[i].Scope = fs.scope // Scope is toml:"-"; set from the store
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
	tmp, err := os.CreateTemp(fs.root, "prefixes-*.toml")
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

// PrefixID derives a stable id from a prefix value (slug). Same value → same id,
// so Add is idempotent.
func PrefixID(value string) string {
	return strings.Trim(strings.ToLower(slugRe.ReplaceAllString(value, "-")), "-")
}

func (fs *FileStore) Add(p model.Prefix) (model.Prefix, error) {
	p.ID = PrefixID(p.Value)
	p.Scope = fs.scope
	if p.Created.IsZero() {
		p.Created = time.Now()
	}
	idx := fs.read()
	for i := range idx.Prefixes {
		if idx.Prefixes[i].ID == p.ID { // same slug → idempotent replace
			idx.Prefixes[i] = p
			return p, fs.write(idx)
		}
	}
	idx.Prefixes = append(idx.Prefixes, p)
	return p, fs.write(idx)
}

func (fs *FileStore) Get(id string) (model.Prefix, error) {
	for _, p := range fs.read().Prefixes {
		if p.ID == id {
			return p, nil
		}
	}
	return model.Prefix{}, ErrNotFound
}

func (fs *FileStore) List() ([]model.Prefix, error) {
	ps := fs.read().Prefixes
	sort.SliceStable(ps, func(a, b int) bool { return ps[a].Created.After(ps[b].Created) })
	return ps, nil
}

func (fs *FileStore) Remove(id string) error {
	idx := fs.read()
	kept := idx.Prefixes[:0]
	found := false
	for _, p := range idx.Prefixes {
		if p.ID == id {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return ErrNotFound
	}
	idx.Prefixes = kept
	return fs.write(idx)
}

var _ Store = (*FileStore)(nil)
