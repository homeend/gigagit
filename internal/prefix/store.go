// Package prefix is gigagit's writable registry of reusable branch-name
// prefixes (skeletons). Two scopes — global (every repo) and repo-specific —
// each a separate file-backed store. The Store interface is the fixed API.
package prefix

import (
	"errors"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Get/Remove for an unknown id.
var ErrNotFound = errors.New("prefix: not found")

// Store persists prefix records for one scope (atomic rewrite, last-writer-wins).
type Store interface {
	Add(p model.Prefix) (model.Prefix, error)
	Get(id string) (model.Prefix, error)
	List() ([]model.Prefix, error)
	Remove(id string) error
}
