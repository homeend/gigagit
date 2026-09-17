package linkhist

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/filelock"
)

// FileStore keeps links.toml under root, rewritten atomically (temp+rename,
// the notes/preview pattern) under a process-local mutex plus a
// links.toml.lock file: the TUI, the CLI and gg web may all record a copied
// link for the same repo.
type FileStore struct {
	root string
	mu   sync.Mutex
}

// NewFileStore roots a store at the per-repo directory (caller-supplied;
// resolving that path against XDG_STATE_HOME is internal/domain's job, not
// this leaf's — see the brief's Gap 3).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

type index struct {
	Links []Entry `toml:"links"`
}

func (fs *FileStore) path() string     { return filepath.Join(fs.root, "links.toml") }
func (fs *FileStore) lockPath() string { return fs.path() + ".lock" }

// read parses the file. A MISSING file reads as empty (nothing has been
// copied yet); every other failure — an unreadable file, corrupt TOML — is
// propagated. Swallowing those would let the next Record rewrite the file
// from an empty base and silently destroy the whole store (the rule
// internal/preview.FileStore.read already states).
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
		return nil, fmt.Errorf("linkhist: %s is corrupt: %w", fs.path(), err)
	}
	return idx.Links, nil
}

// List returns the ring newest-first. It never writes.
func (fs *FileStore) List() ([]Entry, error) { return fs.read() }

// write persists es via temp-file + rename (the notes/preview/bookmark
// pattern).
func (fs *FileStore) write(es []Entry) error {
	data, err := toml.Marshal(index{Links: es})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "links-*.toml")
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

// Record prepends e (dedup-to-top on Link), trims to Max, and persists
// atomically under the process mutex plus the cross-process file lock
// (internal/filelock). A blank Link is a silent no-op: no lock is taken, no
// file is touched.
func (fs *FileStore) Record(e Entry) error {
	if strings.TrimSpace(e.Link) == "" {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := filelock.Acquire(fs.lockPath())
	if err != nil {
		return err
	}
	defer unlock()
	// Read-merge INSIDE the lock: three processes write this file, and
	// read-merge alone (without the file lock) can still lose a sibling's
	// entry when two rewrites interleave. A corrupt file propagates here
	// rather than being treated as empty — swallowing it would let THIS
	// Record destroy the store from an empty base.
	before, err := fs.read()
	if err != nil {
		return err
	}
	merged := make([]Entry, 0, len(before)+1)
	merged = append(merged, e)
	for _, x := range before {
		if x.Link != e.Link { // dedup-to-top: drop the old row, e replaces it
			merged = append(merged, x)
		}
	}
	if len(merged) > Max {
		merged = merged[:Max]
	}
	return fs.write(merged)
}
