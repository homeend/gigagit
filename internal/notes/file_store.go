package notes

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
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
			// Stamp the lock with an owner token and check it before removing:
			// after a stale takeover (below) a SECOND process can be holding a
			// freshly created lock under the same name, and an unconditional
			// release would delete someone else's.
			token := lockToken()
			_, werr := f.WriteString(token)
			cerr := f.Close()
			if werr != nil || cerr != nil {
				// A token-less lock would be released by nobody but the 30 s
				// staleness breaker, so drop it and report the failure.
				os.Remove(fs.lockPath())
				return nil, errors.Join(werr, cerr)
			}
			return func() { releaseLock(fs.lockPath(), token) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, statErr := os.Stat(fs.lockPath()); statErr == nil && time.Since(fi.ModTime()) > lockStale {
			// Stale: the writer died holding it. Retry at once only if the
			// removal actually worked — otherwise (permissions, a Windows
			// share, a racing breaker) fall through to the deadline check and
			// the backoff, so an unremovable stale lock can never spin.
			if rmErr := os.Remove(fs.lockPath()); rmErr == nil {
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, errors.New("notes: notes.toml.lock is held; try again")
		}
		time.Sleep(lockPoll)
	}
}

// lockToken is one lock holder's identity: this process plus a random nonce,
// so two runs of the same pid (or two Stores in one process) never collide.
func lockToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Randomness is a nicety here; the pid alone still beats no token.
		return strconv.Itoa(os.Getpid())
	}
	return strconv.Itoa(os.Getpid()) + "-" + hex.EncodeToString(b[:])
}

// releaseLock removes the lock only while WE still hold it. A lock file whose
// content is someone else's token was taken over after our own was declared
// stale, and removing it would strand that writer without a lock. A lock we
// cannot read at all is left alone too: it is either already gone (nothing to
// do) or unreadable, and the staleness breaker is the backstop either way —
// removing blind would race a writer that re-took it between our two calls.
func releaseLock(path, token string) {
	b, err := os.ReadFile(path)
	if err != nil || string(b) != token {
		return
	}
	os.Remove(path)
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
