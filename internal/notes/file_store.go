package notes

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
)

// FileStore keeps notes.toml under root, rewritten atomically (temp+rename,
// the bookmark pattern) under two locks: a process-local mutex (the sweep
// goroutine vs a `c` keypress in the same process) and a notes.toml.lock file
// (this gg vs another gg, or a phase-2 `gg note add`).
type FileStore struct {
	root string

	mu  sync.Mutex // process-local: guards every mutation AND pol
	pol Policy
}

// NewFileStore roots a store at the per-repo directory (caller-supplied).
func NewFileStore(root string) *FileStore { return &FileStore{root: root} }

// SetPolicy sets the write-time entry cap. Safe to call at any time.
func (fs *FileStore) SetPolicy(p Policy) {
	fs.mu.Lock()
	fs.pol = p
	fs.mu.Unlock()
}

type index struct {
	Notes []model.Note `toml:"notes"`
}

func (fs *FileStore) path() string     { return filepath.Join(fs.root, "notes.toml") }
func (fs *FileStore) lockPath() string { return fs.path() + ".lock" }

// read parses the file. A MISSING file reads as empty (nothing has been
// stored yet); every other failure — an unreadable file, corrupt TOML — is
// propagated. Swallowing those would make the next mutation rewrite the file
// from an empty base and silently destroy the whole store, which is far worse
// than surfacing the error and leaving the file alone.
func (fs *FileStore) read() ([]model.Note, error) {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("notes: %s is corrupt: %w", fs.path(), err)
	}
	return idx.Notes, nil
}

// Load returns every stored note. NEVER writes (not even to prune): the
// startup sweep is the only pruner, so a read-heavy session cannot rewrite
// the file under a concurrent writer.
func (fs *FileStore) Load() ([]model.Note, error) { return fs.read() }

// lock takes the cross-process lock (internal/filelock: an O_EXCL lock file,
// breaking one that has gone stale). Returns a release func.
func (fs *FileStore) lock() (func(), error) { return filelock.Acquire(fs.lockPath()) }

// write persists ns via temp-file + rename (the bookmark/seq-state pattern).
func (fs *FileStore) write(ns []model.Note) error {
	data, err := toml.Marshal(index{Notes: ns})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(fs.root, "notes-*.toml")
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

// mutate is the ONE write path: process mutex → file lock → fresh read →
// apply → drop orphaned replies → cap → atomic rewrite. Re-reading under the
// lock is what makes the startup sweep and a concurrent `gg note add` safe.
//
// A mutation that changes nothing writes nothing: the startup sweep of a
// clean store must not touch the file (nor create it).
//
// It reports how many records the store holds AFTERWARDS — after the orphan
// prune and the entry cap, neither of which the callback can see. Sweep needs
// that to report a truthful drop count.
func (fs *FileStore) mutate(apply func([]model.Note) ([]model.Note, error)) (int, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	before, err := fs.read()
	if err != nil {
		return 0, err
	}
	// apply gets a CLONE: it may edit records in place (Put replaces by ID),
	// and the unchanged-check below has to compare against the state actually
	// read from disk, not against a slice the callback has already rewritten.
	ns, err := apply(slices.Clone(before))
	if err != nil {
		return 0, err
	}
	ns = dropOrphanReplies(ns)
	ns = capOldestFirst(ns, fs.pol.MaxEntries)
	if sameNotes(before, ns) {
		return len(ns), nil
	}
	if werr := fs.write(ns); werr != nil {
		return 0, werr
	}
	return len(ns), nil
}

// sameNotes reports whether two record lists are identical. A nil list and an
// empty one are the same: "no notes" must not become a written empty file.
func sameNotes(a, b []model.Note) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// Put adds n, or replaces the record with the same ID.
func (fs *FileStore) Put(n model.Note) error {
	_, err := fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		for i := range ns {
			if ns[i].ID == n.ID {
				ns[i] = n
				return ns, nil
			}
		}
		return append(ns, n), nil
	})
	return err
}

// Remove deletes one note; removing a root removes its replies too.
func (fs *FileStore) Remove(id string) error {
	_, err := fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		kept := make([]model.Note, 0, len(ns))
		found := false
		for _, n := range ns {
			if n.ID == id || n.ParentID == id {
				found = found || n.ID == id
				continue
			}
			kept = append(kept, n)
		}
		if !found {
			return nil, ErrNotFound
		}
		return kept, nil
	})
	return err
}

// Sweep keeps every note the predicate accepts and reports how many records
// went (including replies orphaned by a dropped root).
//
// A store with no file yet is already swept: Sweep returns without taking the
// lock, so the common startup sweep of a repo that has never had a note
// creates neither the state directory nor the file.
func (fs *FileStore) Sweep(keep func(model.Note) bool) (int, error) {
	if _, err := os.Stat(fs.path()); os.IsNotExist(err) {
		return 0, nil
	}
	before := 0
	// The count is taken from what mutate LEAVES, not from the predicate: the
	// orphan prune and the entry cap drop records the predicate accepted, and
	// a Sweep that reported only its own rejections would under-report the
	// moment the cap bites.
	after, err := fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		before = len(ns)
		kept := make([]model.Note, 0, len(ns))
		for _, n := range ns {
			if keep(n) {
				kept = append(kept, n)
			}
		}
		return kept, nil
	})
	if err != nil {
		return 0, err
	}
	return before - after, nil
}

// dropOrphanReplies removes replies whose root is gone (dropped by a Remove,
// a Sweep predicate, or the cap).
func dropOrphanReplies(ns []model.Note) []model.Note {
	roots := make(map[string]bool, len(ns))
	for _, n := range ns {
		if !n.IsReply() {
			roots[n.ID] = true
		}
	}
	kept := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		if n.IsReply() && !roots[n.ParentID] {
			continue
		}
		kept = append(kept, n)
	}
	return kept
}

// capOldestFirst enforces max records by dropping whole threads, oldest root
// (by Created) first. max <= 0 is uncapped.
func capOldestFirst(ns []model.Note, max int) []model.Note {
	if max <= 0 || len(ns) <= max {
		return ns
	}
	roots := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		if !n.IsReply() {
			roots = append(roots, n)
		}
	}
	sort.SliceStable(roots, func(a, b int) bool { return roots[a].Created.Before(roots[b].Created) })
	doomed := map[string]bool{}
	size := len(ns)
	for _, r := range roots {
		if size <= max {
			break
		}
		doomed[r.ID] = true
		size-- // the root
		for _, n := range ns {
			if n.ParentID == r.ID {
				size--
			}
		}
	}
	kept := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		if doomed[n.ID] || doomed[n.ParentID] {
			continue
		}
		kept = append(kept, n)
	}
	return kept
}

// NewID mints an 8-hex-char id not present in existing.
func NewID(existing []model.Note) string {
	taken := make(map[string]bool, len(existing))
	for _, n := range existing {
		taken[n.ID] = true
	}
	var b [4]byte
	for {
		// crypto/rand.Read never returns an error (Go 1.24+ panics on an
		// unusable source instead), so there is no fallback branch here — one
		// would only be a second, collision-blind id generator.
		rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		if !taken[id] {
			return id
		}
	}
}
