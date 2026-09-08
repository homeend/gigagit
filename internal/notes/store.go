// Package notes is gigagit's machine-local store of review notes: short,
// expiring records anchored to a range of lines on one side of one file at
// one address. It is owned by internal/domain — frontends never import it,
// exactly like shelf/bookmark/prefix.
//
// The store is deliberately narrow: Load never writes, every mutation
// re-reads under a cross-process lock, applies, enforces the entry cap and
// rewrites atomically. Housekeeping (expiry, orphan pruning) is the caller's
// policy, expressed through Sweep's predicate.
package notes

import (
	"errors"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Remove for an unknown id.
var ErrNotFound = errors.New("notes: not found")

// Now is the clock seam (the snapshotNow pattern): tests override it to make
// expiry deterministic. Package-level, so tests that set it run SERIALLY.
var Now = time.Now

// Policy is the write-time budget. MaxEntries <= 0 means uncapped.
type Policy struct{ MaxEntries int }

// Store persists note records. Load is read-only; every other method
// serialises against other processes and other goroutines.
type Store interface {
	Load() ([]model.Note, error)
	Put(n model.Note) error // add, or replace by ID
	Remove(id string) error // a root takes its replies
	Sweep(keep func(model.Note) bool) (dropped int, err error)
	SetPolicy(p Policy) // the write-time budget lives ON the store (§4.4)
}
