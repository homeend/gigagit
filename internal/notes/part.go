package notes

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// Part names one file of the store. The store keeps one file per anchor
// kind so a read that cares about one kind — the working tree, on every
// status refresh — never parses the others (spec 2026-10-04 §3).
type Part string

const (
	PartCommits  Part = "commits"  // every committed note but a merge preview's
	PartPreviews Part = "previews" // notes written in a merge preview ("a...b")
	PartShelf    Part = "shelf"    // notes on shelf entries

	worktreePrefix      = "wt-"
	partUnscoped   Part = "wt-unscoped" // live notes that never recorded a worktree
)

// WorktreePart is the part holding one worktree's live notes: the first 8
// bytes of sha256 over the cleaned top-level path (domain's repoKey formula).
// "" is the unscoped part: such notes match no checkout today and stay
// stored, invisible, until the sweep ages them out.
func WorktreePart(top string) Part {
	if strings.TrimSpace(top) == "" {
		return partUnscoped
	}
	sum := sha256.Sum256([]byte(filepath.Clean(top)))
	return Part(worktreePrefix + hex.EncodeToString(sum[:8]))
}

// PartOf routes a ROOT note. A reply belongs with its root, whose part the
// store looks up (a reply carries no Preview of its own) — see FileStore.Put.
// The live-vs-shared test is domain's worktreeScopedNote: no commit and no
// shelf id.
func PartOf(n model.Note) Part {
	a := n.Address
	switch {
	case a.ShelfID != "":
		return PartShelf
	case a.Commit == "":
		return WorktreePart(a.Worktree)
	case strings.Contains(n.Preview, "..."): // domain.IsPreviewScope
		return PartPreviews
	}
	return PartCommits
}

// IsWorktree reports whether p is one worktree's part.
func (p Part) IsWorktree() bool { return strings.HasPrefix(string(p), worktreePrefix) }

// file is p's path under the store root.
func (p Part) file(root string) string {
	if p.IsWorktree() {
		return filepath.Join(root, "worktrees", strings.TrimPrefix(string(p), worktreePrefix)+".toml")
	}
	return filepath.Join(root, string(p)+".toml")
}

// partOfFile maps an entry of the worktrees/ directory back to its part;
// temp, lock and quarantined files are not parts.
func partOfFile(name string) (Part, bool) {
	key, ok := strings.CutSuffix(name, ".toml")
	if !ok || key == "" || strings.HasPrefix(key, ".") {
		return "", false
	}
	return Part(worktreePrefix + key), true
}
