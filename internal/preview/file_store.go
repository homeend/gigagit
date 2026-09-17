package preview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
)

// FileStore keeps previews.toml under root, rewritten atomically under a
// process-local mutex plus a previews.toml.lock file (TUI, CLI and gg web may
// all write). No entry cap: the list is short and user-curated.
type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type index struct {
	Previews []model.MergePreview `toml:"previews"`
}

func (fs *FileStore) path() string     { return filepath.Join(fs.root, "previews.toml") }
func (fs *FileStore) lockPath() string { return fs.path() + ".lock" }

// read parses the file; a MISSING file is empty, anything else is an error
// (swallowing a corrupt file would let the next write destroy the store).
func (fs *FileStore) read() ([]model.MergePreview, error) {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("preview: %s is corrupt: %w", fs.path(), err)
	}
	return idx.Previews, nil
}

func (fs *FileStore) List() ([]model.MergePreview, error) { return fs.read() }

func (fs *FileStore) Get(id string) (model.MergePreview, error) {
	ps, err := fs.read()
	if err != nil {
		return model.MergePreview{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return model.MergePreview{}, ErrNotFound
}

// lock takes the cross-process lock (internal/filelock: an O_EXCL lock file,
// breaking one that has gone stale). Returns a release func.
func (fs *FileStore) lock() (func(), error) { return filelock.Acquire(fs.lockPath()) }

// write persists ps via temp-file + rename (the bookmark/seq-state pattern).
func (fs *FileStore) write(ps []model.MergePreview) error {
	data, err := toml.Marshal(index{Previews: ps})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "previews-*.toml")
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

// mutate is the ONE write path: mutex → file lock → fresh read → apply →
// atomic rewrite. apply returns the new list, or an error to write nothing.
func (fs *FileStore) mutate(apply func([]model.MergePreview) ([]model.MergePreview, error)) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return err
	}
	defer unlock()
	before, err := fs.read()
	if err != nil {
		return err
	}
	after, err := apply(slices.Clone(before))
	if err != nil {
		return err
	}
	return fs.write(after)
}

func (fs *FileStore) Add(p model.MergePreview) (model.MergePreview, error) {
	p.ID = ID(p.Source, p.Target)
	if p.Label == "" {
		p.Label = p.DefaultLabel()
	}
	if p.Created.IsZero() {
		p.Created = time.Now().UTC()
	}
	var existing *model.MergePreview
	err := fs.mutate(func(ps []model.MergePreview) ([]model.MergePreview, error) {
		for _, q := range ps {
			if q.ID == p.ID {
				e := q
				existing = &e
				return nil, ErrExists
			}
		}
		return append(ps, p), nil
	})
	if errors.Is(err, ErrExists) {
		return *existing, err
	}
	if err != nil {
		return model.MergePreview{}, err
	}
	return p, nil
}

func (fs *FileStore) Rename(id, label string) error {
	return fs.mutate(func(ps []model.MergePreview) ([]model.MergePreview, error) {
		for i := range ps {
			if ps[i].ID == id {
				ps[i].Label = label
				return ps, nil
			}
		}
		return nil, ErrNotFound
	})
}

func (fs *FileStore) Remove(id string) error {
	return fs.mutate(func(ps []model.MergePreview) ([]model.MergePreview, error) {
		kept := ps[:0:0]
		found := false
		for _, p := range ps {
			if p.ID == id {
				found = true
				continue
			}
			kept = append(kept, p)
		}
		if !found {
			return nil, ErrNotFound
		}
		return kept, nil
	})
}
