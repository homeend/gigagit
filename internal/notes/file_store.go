package notes

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/model"
)

const (
	lockWait  = 2 * time.Second       // total retry budget for the cross-process lock
	lockPoll  = 20 * time.Millisecond // retry interval
	lockStale = 30 * time.Second      // a lock older than this is a crashed writer's
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

// read parses the file. A missing or corrupt file reads as empty — a note
// store is working material, never worth failing a whole session over.
func (fs *FileStore) read() []model.Note {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		return nil
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return nil
	}
	return idx.Notes
}

// Load returns every stored note. NEVER writes (not even to prune): the
// startup sweep is the only pruner, so a read-heavy session cannot rewrite
// the file under a concurrent writer.
func (fs *FileStore) Load() ([]model.Note, error) { return fs.read(), nil }

// lock takes the cross-process lock, breaking one that is older than
// lockStale (a crashed writer). Returns a release func.
func (fs *FileStore) lock() (func(), error) {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return nil, err
	}
	// The retry budget uses the REAL clock, never the Now seam: a test that
	// freezes Now for expiry must not spin here forever on a held lock.
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(fs.lockPath(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(fs.lockPath()) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, statErr := os.Stat(fs.lockPath()); statErr == nil && time.Since(fi.ModTime()) > lockStale {
			os.Remove(fs.lockPath()) // stale: the writer died holding it
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("notes: notes.toml.lock is held; try again")
		}
		time.Sleep(lockPoll)
	}
}

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
func (fs *FileStore) mutate(apply func([]model.Note) ([]model.Note, error)) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return err
	}
	defer unlock()
	ns, err := apply(fs.read())
	if err != nil {
		return err
	}
	ns = dropOrphanReplies(ns)
	ns = capOldestFirst(ns, fs.pol.MaxEntries)
	return fs.write(ns)
}

// Put adds n, or replaces the record with the same ID.
func (fs *FileStore) Put(n model.Note) error {
	return fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		for i := range ns {
			if ns[i].ID == n.ID {
				ns[i] = n
				return ns, nil
			}
		}
		return append(ns, n), nil
	})
}

// Remove deletes one note; removing a root removes its replies too.
func (fs *FileStore) Remove(id string) error {
	return fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		kept := ns[:0]
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
}

// Sweep keeps every note the predicate accepts and reports how many records
// went (including replies orphaned by a dropped root).
func (fs *FileStore) Sweep(keep func(model.Note) bool) (int, error) {
	dropped := 0
	err := fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		before := len(ns)
		kept := ns[:0]
		for _, n := range ns {
			if keep(n) {
				kept = append(kept, n)
			}
		}
		dropped = before - len(dropOrphanReplies(kept))
		return kept, nil
	})
	if err != nil {
		return 0, err
	}
	return dropped, nil
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
		if _, err := rand.Read(b[:]); err != nil {
			// crypto/rand cannot fail in practice; fall back to the clock so a
			// note is still storable rather than lost.
			return hex.EncodeToString([]byte{
				byte(Now().UnixNano()), byte(Now().UnixNano() >> 8),
				byte(Now().UnixNano() >> 16), byte(Now().UnixNano() >> 24)})
		}
		id := hex.EncodeToString(b[:])
		if !taken[id] {
			return id
		}
	}
}
