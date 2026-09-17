// Package linkhist is gigagit's per-repo MRU of gg:// links the user has
// COPIED: records only, no blobs. Owned by internal/domain — frontends reach
// it only through domain queries (the notes/shelf/preview/bookmark
// convention).
package linkhist

// Max is the hard ceiling on kept entries. Twenty is a list a human can read
// without scrolling and is the spec's figure (§4.3).
const Max = 20

// Entry is one copied link.
type Entry struct {
	// Link is the canonical link text (model.Link.String()). It is the
	// identity: re-copying the same text moves the row to the top rather than
	// adding one.
	Link string `toml:"link"`
	// Desc is the human label, captured AT CREATION and never derived later
	// (ruling R7): the describing context — which row the user was on — is
	// gone by the time anything lists this. A stash's subject is the only
	// thing that makes its row recognisable, because stash@{N} is
	// deliberately absent from the link.
	Desc string `toml:"desc"`
	// Created is RFC3339. Display-only; order is the slice's.
	Created string `toml:"created"`
}

// Store persists one repo's ring, newest first.
type Store interface {
	// List returns the ring newest-first. Empty when there is none.
	List() ([]Entry, error)
	// Record prepends e (dedup-to-top on Link), trims to Max, and persists
	// atomically under a cross-process lock. A blank Link is a no-op, not an
	// error. An existing row's Desc is REPLACED by the new one: the surface
	// the user copied from this time is the one they will recognise.
	Record(e Entry) error
}

var _ Store = (*FileStore)(nil)
