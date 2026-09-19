// Package savedcompare is gigagit's machine-local registry of saved
// comparisons. One entry is either a PAIR (two links: "these two things,
// compared") or a SET (one link, Right nil: everything that link changes —
// which is what a saved merge preview is). Records only, no blobs. Owned by
// internal/domain — frontends never import it (like notes/bookmark/shelf).
package savedcompare

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// ErrNotFound is returned by Get/Rename/Remove for an unknown id.
var ErrNotFound = errors.New("savedcompare: not found")

// ErrExists is returned by Add when the (Left, Right) pair is already stored;
// the existing record is returned alongside it.
var ErrExists = errors.New("savedcompare: already exists")

// Entry is one saved comparison.
//
// Right == nil means a saved SET, not a pair: a merge preview is one bounded
// set ("everything feat/x would bring into main"), and the store holds that
// shape explicitly rather than faking a second side (spec §4.5).
//
// The links are stored UNRESOLVED, exactly as produced. That is what makes a
// saved preview follow its branches as they move: `gg://r@main...feat/x`
// names two branches, and every open re-resolves their tips.
type Entry struct {
	ID      string      `toml:"id"`
	Left    model.Link  `toml:"left"`
	Right   *model.Link `toml:"right,omitempty"`
	Label   string      `toml:"label"`
	Created time.Time   `toml:"created"`
}

// IsSet reports whether this entry is a bounded SET rather than a pair.
func (e Entry) IsSet() bool { return e.Right == nil }

// ID derives a record's id from its two link TEXTS: sha256("left\x00right"),
// first 8 hex chars. Direction-sensitive, like preview.ID before it.
//
// ONE derivation serves both shapes — a set hashes an EMPTY right half rather
// than taking a second code path. A hash that branched on shape would be the
// signature two-arms defect of this feature: two arms that look alike and
// must behave differently, with the rule applied to one and skipped for the
// other.
//
// Entries converted from the legacy previews store keep their OLD id instead
// (see ConvertLegacy): ids are user-visible handles typed into `gg preview
// <id>`, so they are carried across rather than recomputed.
func ID(left, right string) string {
	sum := sha256.Sum256([]byte(left + "\x00" + right))
	return hex.EncodeToString(sum[:4])
}

// targetLabel names one link's target the way a user would say it. It reads
// the TARGET FIELDS rather than slicing the rendered link, because the repo
// half of a link may be an absolute path containing any character the slicing
// would have to guess at.
func targetLabel(l model.Link) string {
	t := l.Target
	switch {
	case t.Preview != nil:
		return t.Preview.Target + "..." + t.Preview.Source
	case t.Pair != nil:
		return t.Pair.A + ".." + t.Pair.B
	case t.Ref != "":
		return "ref:" + t.Ref
	case t.Commit != "":
		if len(t.Commit) > 7 {
			return t.Commit[:7]
		}
		return t.Commit
	case t.State == model.StateStaged:
		return "staged"
	default:
		return "worktree"
	}
}

// DefaultLabel is the label an entry gets when the caller gives none.
func (e Entry) DefaultLabel() string {
	left := targetLabel(e.Left)
	if e.Right == nil {
		return left
	}
	return left + " <-> " + targetLabel(*e.Right)
}

// Store persists saved comparisons. Every mutation re-reads under a
// cross-process lock, applies, and rewrites atomically.
type Store interface {
	Add(e Entry) (Entry, error) // fills ID (+ Label when empty); ErrExists with the stored record
	Get(id string) (Entry, error)
	List() ([]Entry, error) // insertion order
	Rename(id, label string) error
	Remove(id string) error
}

var _ Store = (*FileStore)(nil)
