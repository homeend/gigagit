// Package shelf is gigagit's non-git, per-file content store: frozen,
// content-addressed copies of files that survive permanent deletion of the
// source. The Store interface is the fixed API; the file-backed implementation
// is swappable (e.g. a future embedded-KV backend).
package shelf

import (
	"errors"

	"github.com/gigagit/gg/internal/model"
)

// MaxShelfBytes caps a single shelved file (mirrors domain.MaxDiffBytes).
const MaxShelfBytes = 10 << 20

// DefaultBucket is the implicit bucket addressed by "" or "default".
const DefaultBucket = "default"

// ErrTooLarge is returned by Put when content exceeds MaxShelfBytes.
var ErrTooLarge = errors.New("shelf: file exceeds size limit")

// ErrNotFound is returned by Get/Remove for an unknown entry id.
var ErrNotFound = errors.New("shelf: entry not found")

// Store persists shelved files. Implementations are safe for sequential use by
// one process; cross-process writes are last-writer-wins (atomic index rewrite).
type Store interface {
	Put(bucket string, addr model.FileAddress, data []byte) (model.ShelfEntry, error)
	Get(entryID string) ([]byte, error)
	List(bucket string, skip, limit int) ([]model.ShelfEntry, error)
	Buckets() ([]model.ShelfBucket, error)
	Remove(entryID string) error
}
