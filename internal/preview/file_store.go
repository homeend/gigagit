package preview

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// lock takes the cross-process lock, breaking one that is older than
// lockStale (a crashed writer). Returns a release func.
func (fs *FileStore) lock() (func(), error) {
	if err := os.MkdirAll(fs.root, 0o755); err != nil {
		return nil, err
	}
	// The retry budget uses the REAL clock, never the Now seam: a test that
	// freezes Now for expiry must not spin here forever on a held lock.
	deadline := time.Now().Add(lockWait)
	// last is the most recent reason the create failed, so the deadline error
	// says WHICH wall we hit — a held lock reads differently from a name
	// Windows is still tearing down.
	var last error
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
		// ErrExist is the ordinary "someone holds it" answer. ErrPermission
		// must ALSO retry, and only Windows shows why: os.Remove there marks a
		// file whose handle someone still holds (an on-access virus scanner,
		// the search indexer) as PENDING DELETE rather than unlinking it, and
		// an O_EXCL create against that name then fails with
		// ERROR_ACCESS_DENIED — not ErrExist. Aborting on it failed a write
		// outright for a condition that clears in milliseconds; the Windows
		// suite caught it as "Access is denied" from a Put whose lock had
		// already been released. Anything else is a real defect and still
		// aborts at once.
		if !errors.Is(err, os.ErrExist) && !errors.Is(err, os.ErrPermission) {
			return nil, err
		}
		// A pending-delete name also refuses os.Stat, so the staleness check
		// below simply does not fire for one — which is correct: a lock nobody
		// holds any more is not a stale lock to break, it is a name to retry.
		last = err
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
			return nil, fmt.Errorf("preview: previews.toml.lock is held; try again (last: %v)", last)
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
// cannot read at all is removed anyway: we wrote it a moment ago, so a read
// error is far likelier a transient (drvfs/9p) than a takeover, and leaving
// it would stall every writer for the 30 s stale window; the Remove is a
// no-op if it is already gone. The takeover race that leaves is a
// microsecond gap behind a lock that was ALREADY 30 s stale.
func releaseLock(path, token string) {
	if b, err := os.ReadFile(path); err == nil && string(b) != token {
		return
	}
	os.Remove(path)
}

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
