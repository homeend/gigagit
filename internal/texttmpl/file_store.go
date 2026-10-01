package texttmpl

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

// FileStore keeps an atomic-rewrite TOML registry under
// root/texttemplates.toml, all rows belonging to one scope.
type FileStore struct {
	root  string
	scope model.ProfileScope
}

// NewFileStore roots a store at a scope's directory (caller-supplied).
func NewFileStore(root string, scope model.ProfileScope) *FileStore {
	return &FileStore{root: root, scope: scope}
}

type index struct {
	Templates []model.TextTemplate `toml:"templates"`
}

func (fs *FileStore) path() string { return filepath.Join(fs.root, "texttemplates.toml") }

func (fs *FileStore) read() index {
	var idx index
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return idx
	}
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}
	}
	for i := range idx.Templates {
		idx.Templates[i].Scope = fs.scope // Scope is toml:"-"; set from the store
	}
	return idx
}

// write persists idx via temp-file + rename (the prefix-store pattern).
func (fs *FileStore) write(idx index) error {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "texttemplates-*.toml")
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

var slugRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// ID derives a template's id from its title: lower-cased, every run of
// non-letter/non-digit runes becomes one '-'. "" when the title has neither.
func ID(title string) string {
	return strings.Trim(strings.ToLower(slugRe.ReplaceAllString(title, "-")), "-")
}

func (fs *FileStore) Add(t model.TextTemplate) (model.TextTemplate, error) {
	t.ID = ID(t.Title)
	if t.ID == "" {
		return model.TextTemplate{}, ErrNoID
	}
	t.Scope = fs.scope
	if t.Created.IsZero() {
		t.Created = time.Now()
	}
	idx := fs.read()
	for _, have := range idx.Templates {
		if have.ID == t.ID {
			return model.TextTemplate{}, ErrDuplicate
		}
	}
	idx.Templates = append(idx.Templates, t)
	return t, fs.write(idx)
}

func (fs *FileStore) Get(id string) (model.TextTemplate, error) {
	for _, t := range fs.read().Templates {
		if t.ID == id {
			return t, nil
		}
	}
	return model.TextTemplate{}, ErrNotFound
}

// List returns the rows alphabetically by title (case-insensitive).
func (fs *FileStore) List() ([]model.TextTemplate, error) {
	ts := fs.read().Templates
	sort.SliceStable(ts, func(a, b int) bool {
		return strings.ToLower(ts[a].Title) < strings.ToLower(ts[b].Title)
	})
	return ts, nil
}

func (fs *FileStore) Update(id string, t model.TextTemplate) (model.TextTemplate, error) {
	newID := ID(t.Title)
	if newID == "" {
		return model.TextTemplate{}, ErrNoID
	}
	idx := fs.read()
	at := -1
	for i, have := range idx.Templates {
		if have.ID == id {
			at = i
		} else if have.ID == newID {
			return model.TextTemplate{}, ErrDuplicate
		}
	}
	if at < 0 {
		return model.TextTemplate{}, ErrNotFound
	}
	t.ID, t.Scope, t.Created = newID, fs.scope, idx.Templates[at].Created
	idx.Templates[at] = t
	return t, fs.write(idx)
}

func (fs *FileStore) Remove(id string) error {
	idx := fs.read()
	kept := idx.Templates[:0]
	found := false
	for _, t := range idx.Templates {
		if t.ID == id {
			found = true
			continue
		}
		kept = append(kept, t)
	}
	if !found {
		return ErrNotFound
	}
	idx.Templates = kept
	return fs.write(idx)
}

var _ Store = (*FileStore)(nil)
