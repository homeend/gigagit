// Package profile is gigagit's writable registry of named git-identity
// presets ("app profiles"). It has two scopes — global (every repo) and
// repo-specific — each a separate file-backed store. The Store interface is
// the fixed API; the file-backed implementation is swappable.
package profile

import (
	"errors"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Get/Remove for an unknown id.
var ErrNotFound = errors.New("profile: not found")

// Store persists profile records for one scope. Safe for sequential use by
// one process; cross-process writes are last-writer-wins (atomic rewrite).
type Store interface {
	Add(p model.Profile) (model.Profile, error)
	Get(id string) (model.Profile, error)
	List() ([]model.Profile, error)
	Remove(id string) error
}
