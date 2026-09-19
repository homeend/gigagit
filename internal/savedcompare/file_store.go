package savedcompare

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
)

// FileStore keeps savedcompare.toml under root, rewritten atomically under a
// process-local mutex plus a savedcompare.toml.lock file (TUI, CLI and gg web
// may all write). No entry cap: the list is short and user-curated.
type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type index struct {
	Entries []Entry `toml:"entries"`
}

func (fs *FileStore) path() string     { return filepath.Join(fs.root, "savedcompare.toml") }
func (fs *FileStore) lockPath() string { return fs.path() + ".lock" }

// read parses the file; a MISSING file is empty, anything else is an error
// (swallowing a corrupt file would let the next write destroy the store).
func (fs *FileStore) read() ([]Entry, error) {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("savedcompare: %s is corrupt: %w", fs.path(), err)
	}
	return idx.Entries, nil
}

func (fs *FileStore) List() ([]Entry, error) { return fs.read() }

func (fs *FileStore) Get(id string) (Entry, error) {
	es, err := fs.read()
	if err != nil {
		return Entry{}, err
	}
	for _, e := range es {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, ErrNotFound
}

// lock takes the cross-process lock (internal/filelock: an O_EXCL lock file,
// breaking one that has gone stale). Returns a release func.
func (fs *FileStore) lock() (func(), error) { return filelock.Acquire(fs.lockPath()) }

// write persists es via temp-file + rename (the bookmark/seq-state pattern).
func (fs *FileStore) write(es []Entry) error {
	data, err := toml.Marshal(index{Entries: es})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "savedcompare-*.toml")
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
func (fs *FileStore) mutate(apply func([]Entry) ([]Entry, error)) error {
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

// rightText renders an entry's right half for identity purposes. A SET has an
// empty one — the same empty string ID() hashes, so one derivation serves
// both shapes.
func rightText(e Entry) string {
	if e.Right == nil {
		return ""
	}
	return e.Right.String()
}

func (fs *FileStore) Add(e Entry) (Entry, error) {
	right := rightText(e)
	if e.ID == "" {
		e.ID = ID(e.Left.String(), right)
	}
	if e.Label == "" {
		e.Label = e.DefaultLabel()
	}
	if e.Created.IsZero() {
		e.Created = time.Now().UTC()
	}
	var existing *Entry
	err := fs.mutate(func(es []Entry) ([]Entry, error) {
		for _, q := range es {
			// Dedup on the PAIR, never on the id: a converted entry carries a
			// legacy id, so two rows for the same comparison could hold
			// different ids and an id-keyed check would miss the duplicate.
			if q.Left.String() == e.Left.String() && rightText(q) == right {
				c := q
				existing = &c
				return nil, ErrExists
			}
		}
		return append(es, e), nil
	})
	if errors.Is(err, ErrExists) {
		return *existing, err
	}
	if err != nil {
		return Entry{}, err
	}
	return e, nil
}

func (fs *FileStore) Rename(id, label string) error {
	return fs.mutate(func(es []Entry) ([]Entry, error) {
		for i := range es {
			if es[i].ID == id {
				es[i].Label = label
				return es, nil
			}
		}
		return nil, ErrNotFound
	})
}

func (fs *FileStore) Remove(id string) error {
	return fs.mutate(func(es []Entry) ([]Entry, error) {
		kept := es[:0:0]
		found := false
		for _, e := range es {
			if e.ID == id {
				found = true
				continue
			}
			kept = append(kept, e)
		}
		if !found {
			return nil, ErrNotFound
		}
		return kept, nil
	})
}
