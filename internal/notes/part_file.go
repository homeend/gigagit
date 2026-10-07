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

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
)

// partFile is ONE file of the store, rewritten atomically (temp+rename, the
// bookmark pattern) under two locks: a process-local mutex (the sweep
// goroutine vs a `c` keypress in the same process) and a <file>.lock file
// (this gg vs another gg, or a `gg note add`). FileStore composes one per
// part; every rule of the pre-split notes.toml lives here unchanged.
type partFile struct {
	path string // the .toml file

	mu  sync.Mutex // process-local: guards every mutation AND pol
	pol Policy
}

// SetPolicy sets the write-time entry cap. Safe to call at any time.
func (fs *partFile) SetPolicy(p Policy) {
	fs.mu.Lock()
	fs.pol = p
	fs.mu.Unlock()
}

// ErrCorrupt is wrapped by a read of a part file that is not valid TOML.
var ErrCorrupt = errors.New("notes: store is corrupt")

type index struct {
	Notes    []model.Note             `toml:"notes"`
	Resolved []model.ThreadResolution `toml:"resolved,omitempty"`
}

func (fs *partFile) lockPath() string { return fs.path + ".lock" }

// read parses the file. A MISSING file reads as empty (nothing has been
// stored yet); every other failure — an unreadable file, corrupt TOML — is
// propagated. Swallowing those would make the next mutation rewrite the file
// from an empty base and silently destroy the whole store, which is far worse
// than surfacing the error and leaving the file alone.
func (fs *partFile) read() ([]model.Note, error) {
	idx, err := fs.readIndex()
	return idx.Notes, err
}

// readIndex parses the whole file: the notes and the thread resolutions
// (read's missing-file and corrupt-file rules).
func (fs *partFile) readIndex() (index, error) {
	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return index{}, nil
		}
		return index{}, err
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}, fmt.Errorf("%w: %s: %v", ErrCorrupt, fs.path, err)
	}
	return idx, nil
}

// LoadResolved returns this part's thread resolutions. Never writes.
func (fs *partFile) LoadResolved() ([]model.ThreadResolution, error) {
	idx, err := fs.readIndex()
	return idx.Resolved, err
}

// Load returns every note of this part. NEVER writes (not even to prune): the
// startup sweep is the only pruner, so a read-heavy session cannot rewrite
// the file under a concurrent writer.
func (fs *partFile) Load() ([]model.Note, error) { return fs.read() }

// lock takes the cross-process lock (internal/filelock: an O_EXCL lock file,
// breaking one that has gone stale). Returns a release func.
func (fs *partFile) lock() (func(), error) { return filelock.Acquire(fs.lockPath()) }

// write persists ns via temp-file + rename (the bookmark/seq-state pattern).
// A part left with no notes is removed instead of written empty, so a
// removed worktree leaves no file behind. The temp name never ends in .toml:
// a concurrent part listing must not mistake it for a part.
func (fs *partFile) write(idx index) error {
	if len(idx.Notes) == 0 { // a resolution cannot outlive its root
		if err := os.Remove(fs.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := toml.Marshal(idx)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(fs.path), ".notes-*.tmp")
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
	if err := os.Rename(name, fs.path); err != nil {
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
func (fs *partFile) mutate(apply func([]model.Note) ([]model.Note, error)) (int, error) {
	return fs.mutateCap(apply, true)
}

// mutateCap is mutate with the entry cap optional: the legacy conversion
// merges uncapped, because a lossless migration must not drop a thread.
func (fs *partFile) mutateCap(apply func([]model.Note) ([]model.Note, error), capped bool) (int, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return 0, err
	}
	defer unlock()
	before, err := fs.readIndex()
	if err != nil {
		return 0, err
	}
	// apply gets a CLONE: it may edit records in place (Put replaces by ID),
	// and the unchanged-check below has to compare against the state actually
	// read from disk, not against a slice the callback has already rewritten.
	ns, err := apply(slices.Clone(before.Notes))
	if err != nil {
		return 0, err
	}
	ns = dropOrphanReplies(ns)
	if capped {
		ns = capOldestFirst(ns, fs.pol.MaxEntries)
	}
	// A resolution goes with its thread root, whatever removed the root.
	rs := keepRootedResolutions(before.Resolved, ns)
	if sameNotes(before.Notes, ns) && sameResolutions(before.Resolved, rs) {
		return len(ns), nil
	}
	if werr := fs.write(index{Notes: ns, Resolved: rs}); werr != nil {
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

// keepRootedResolutions drops a resolution whose thread root is gone from
// the part: a stored root by id, a review remark by its review.
func keepRootedResolutions(rs []model.ThreadResolution, ns []model.Note) []model.ThreadResolution {
	if len(rs) == 0 {
		return rs
	}
	roots := make(map[string]bool, len(ns))
	for _, n := range ns {
		if !n.IsReply() {
			roots[n.ID] = true
		}
	}
	kept := make([]model.ThreadResolution, 0, len(rs))
	for _, r := range rs {
		if roots[model.StoredRootID(r.Root)] {
			kept = append(kept, r)
		}
	}
	return kept
}

// sameResolutions: nil and empty are the same (nothing to write).
func sameResolutions(a, b []model.ThreadResolution) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mutateResolved is mutate for the resolutions: same locks, fresh read,
// atomic rewrite; the notes are untouched (apply sees them to check a
// root). A resolution whose root is not in this part is dropped.
func (fs *partFile) mutateResolved(apply func(ns []model.Note, rs []model.ThreadResolution) ([]model.ThreadResolution, error)) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return err
	}
	defer unlock()
	before, err := fs.readIndex()
	if err != nil {
		return err
	}
	rs, err := apply(before.Notes, slices.Clone(before.Resolved))
	if err != nil {
		return err
	}
	rs = keepRootedResolutions(rs, before.Notes)
	if sameResolutions(before.Resolved, rs) {
		return nil
	}
	return fs.write(index{Notes: before.Notes, Resolved: rs})
}

// Resolve adds r, or replaces the entry with the same Root. ErrNotFound when
// the root is not in this part.
func (fs *partFile) Resolve(r model.ThreadResolution) error {
	return fs.mutateResolved(func(ns []model.Note, rs []model.ThreadResolution) ([]model.ThreadResolution, error) {
		if !hasRoot(ns, model.StoredRootID(r.Root)) {
			return nil, ErrNotFound
		}
		for i := range rs {
			if rs[i].Root == r.Root {
				rs[i] = r
				return rs, nil
			}
		}
		return append(rs, r), nil
	})
}

// Unresolve removes root's entry; ErrNotFound when there is none.
func (fs *partFile) Unresolve(root string) error {
	return fs.mutateResolved(func(_ []model.Note, rs []model.ThreadResolution) ([]model.ThreadResolution, error) {
		i := slices.IndexFunc(rs, func(r model.ThreadResolution) bool { return r.Root == root })
		if i < 0 {
			return nil, ErrNotFound
		}
		return slices.Delete(rs, i, i+1), nil
	})
}

func hasRoot(ns []model.Note, id string) bool {
	return slices.ContainsFunc(ns, func(n model.Note) bool { return n.ID == id && !n.IsReply() })
}

// Put adds n, or replaces the record with the same ID.
func (fs *partFile) Put(n model.Note) error {
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

// edit rewrites the one record id under the part's lock; ErrNotFound when
// this part does not hold it. fn's error aborts with nothing written.
func (fs *partFile) edit(id string, fn func(*model.Note) error) error {
	_, err := fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		for i := range ns {
			if ns[i].ID == id {
				err := fn(&ns[i])
				switch {
				case errors.Is(err, ErrRemoveRecord):
					kept := make([]model.Note, 0, len(ns))
					for _, n := range ns {
						if n.ID != id && n.StoredParent() != id {
							kept = append(kept, n)
						}
					}
					return kept, nil
				case err != nil:
					return nil, err
				}
				return ns, nil
			}
		}
		return nil, ErrNotFound
	})
	return err
}

// Remove deletes one note; removing a root removes its replies too — a
// review takes its remark replies (their stored parent is the review).
func (fs *partFile) Remove(id string) error {
	_, err := fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		kept := make([]model.Note, 0, len(ns))
		found := false
		for _, n := range ns {
			if n.ID == id || n.StoredParent() == id {
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
func (fs *partFile) Sweep(keep func(model.Note) bool) (int, error) {
	if _, err := os.Stat(fs.path); os.IsNotExist(err) {
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
		// A draft answer to a GitHub thread has its root on the forge, never
		// in the store: it is not an orphan.
		if n.IsReply() && !n.IsForgeReply() && !roots[n.StoredParent()] {
			continue
		}
		kept = append(kept, n)
	}
	return kept
}

// capOldestFirst enforces max records by dropping whole threads, oldest root
// (by Created) first. max <= 0 is uncapped.
//
// Entry-level notes (AI reviews, shelf-entry annotations) are exempt: they neither count toward max
// nor are ever dropped, and neither are their replies.
func capOldestFirst(ns []model.Note, max int) []model.Note {
	if max <= 0 {
		return ns
	}
	exempt := map[string]bool{}
	for _, n := range ns {
		if !n.IsReply() && n.IsEntryLevel() {
			exempt[n.ID] = true
		}
	}
	size := 0
	for _, n := range ns {
		if !exempt[n.ID] && !exempt[n.StoredParent()] {
			size++
		}
	}
	if size <= max {
		return ns
	}
	roots := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		// A forge-rooted reply is a thread of its own (nothing hangs off it).
		if (!n.IsReply() || n.IsForgeReply()) && !exempt[n.ID] {
			roots = append(roots, n)
		}
	}
	sort.SliceStable(roots, func(a, b int) bool { return roots[a].Created.Before(roots[b].Created) })
	doomed := map[string]bool{}
	for _, r := range roots {
		if size <= max {
			break
		}
		doomed[r.ID] = true
		size-- // the root
		for _, n := range ns {
			if n.StoredParent() == r.ID {
				size--
			}
		}
	}
	kept := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		if doomed[n.ID] || doomed[n.StoredParent()] {
			continue
		}
		kept = append(kept, n)
	}
	return kept
}

// Quarantine moves the file aside to <file>.corrupt-<unix> under its locks and reports where it went ("" when there was no file). The
// next write starts a fresh store; nothing is deleted.
func (fs *partFile) Quarantine() (string, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err := os.Stat(fs.path); os.IsNotExist(err) {
		return "", nil
	}
	dst := fs.path + ".corrupt-" + strconv.FormatInt(Now().Unix(), 10)
	if err := os.Rename(fs.path, dst); err != nil {
		return "", err
	}
	return dst, nil
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
