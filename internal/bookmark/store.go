// Package bookmark is gigagit's persistent registry of richly-addressed file
// bookmarks: a record store of pointers (no byte content). The Store interface
// is the fixed API; the file-backed implementation is swappable.
package bookmark

import (
	"errors"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Get/Remove for an unknown id.
var ErrNotFound = errors.New("bookmark: not found")

// Store persists bookmark records. Safe for sequential use by one process;
// cross-process writes are last-writer-wins (atomic index rewrite).
type Store interface {
	Add(b model.Bookmark) (model.Bookmark, error)
	Get(id string) (model.Bookmark, error)
	List(skip, limit int) ([]model.Bookmark, error)
	Remove(id string) error
}
