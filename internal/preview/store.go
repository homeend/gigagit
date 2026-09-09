// Package preview is gigagit's machine-local registry of saved merge previews:
// (source, target) branch-name pairs whose diff is recomputed from the live
// tips every time one is opened. Records only, no blobs. Owned by
// internal/domain — frontends never import it (like notes/bookmark/shelf).
package preview

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Get/Rename/Remove for an unknown id.
var ErrNotFound = errors.New("preview: not found")

// ErrExists is returned by Add when the (source, target) pair is already
// stored; the existing record is returned alongside it.
var ErrExists = errors.New("preview: already exists")

// ID derives a record's id from its pair: sha256("source\x00target"), first 8
// hex chars. Direction-sensitive — main→feat/x and feat/x→main are two ids.
func ID(source, target string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + target))
	return hex.EncodeToString(sum[:4])
}

// Store persists preview records. Every mutation re-reads under a
// cross-process lock, applies, and rewrites atomically.
type Store interface {
	Add(p model.MergePreview) (model.MergePreview, error) // fills ID (+ Label when empty); ErrExists with the stored record
	Get(id string) (model.MergePreview, error)
	List() ([]model.MergePreview, error) // insertion order
	Rename(id, label string) error
	Remove(id string) error
}

var _ Store = (*FileStore)(nil)
