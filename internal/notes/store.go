// Package notes is gigagit's machine-local store of review notes: short,
// expiring records anchored to a range of lines on one side of one file at
// one address. It is owned by internal/domain — frontends never import it,
// exactly like shelf/bookmark/prefix.
//
// The store keeps one file per anchor kind (part.go) and is deliberately
// narrow: Load never writes, every mutation re-reads ITS part under a
// cross-process lock, applies, enforces the entry cap and rewrites
// atomically. Housekeeping (expiry, orphan pruning) is the caller's
// policy, expressed through Sweep's predicate.
package notes

import (
	"errors"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Remove for an unknown id.
var ErrNotFound = errors.New("notes: not found")

// ErrRemoveRecord, returned by an Edit's fn, removes the record (a root takes
// its replies) in the same locked write that judged it — a decision to
// delete never races a reply written between two calls.
var ErrRemoveRecord = errors.New("notes: remove this record")

// Now is the clock seam (the snapshotNow pattern): tests override it to make
// expiry deterministic. Package-level, so tests that set it run SERIALLY.
var Now = clock.Now

// Policy is the write-time budget. MaxEntries <= 0 means uncapped.
type Policy struct{ MaxEntries int }

// Store persists note records in parts (spec 2026-10-04): Load reads one
// part, LoadAll every part; neither writes. Put routes by PartOf (a reply by
// its root); every other method serialises against other processes and
// other goroutines, per part.
type Store interface {
	Load(p Part) ([]model.Note, error)
	LoadAll() ([]model.Note, error)
	Put(n model.Note) error // add, or replace by ID
	Remove(id string) error // a root takes its replies
	// Edit is load+edit+save of ONE record under its part's lock;
	// ErrNotFound when no part holds id (fn's error aborts the write;
	// ErrRemoveRecord removes the record and its replies instead).
	Edit(id string, fn func(*model.Note) error) error
	Sweep(keep func(model.Note) bool) (dropped int, err error)
	SetPolicy(p Policy) // the write-time budget lives ON the store (§4.4)
	// LoadResolved / LoadAllResolved read the thread resolutions (never
	// write). Resolve records a thread resolved in its root's part (a review
	// remark's: its review's); ErrNotFound when no part holds the root.
	// Unresolve removes the entry; ErrNotFound when there is none.
	LoadResolved(p Part) ([]model.ThreadResolution, error)
	LoadAllResolved() ([]model.ThreadResolution, error)
	Resolve(r model.ThreadResolution) error
	Unresolve(root string) error
}

var _ Store = (*FileStore)(nil)
