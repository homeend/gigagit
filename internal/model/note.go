package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// NoteSource is who wrote a note. The TUI's `a` key hides the agent layer;
// user notes are always visible (hunk's policy).
type NoteSource string

const (
	NoteSourceUser  NoteSource = "user"
	NoteSourceAgent NoteSource = "agent"
	// NoteSourceForge is a review comment read from a forge (a pull request's
	// inline or file-level thread). It is never stored and never editable: gg
	// reads the forge, it does not write to it.
	NoteSourceForge NoteSource = "forge"
)

// ForgeNoteIDPrefix marks the id of a note converted from a forge comment.
// Store ids never carry it, so the two id spaces cannot collide.
const ForgeNoteIDPrefix = "forge:"

// IsForgeNoteID reports whether id names a forge comment, not a stored note.
func IsForgeNoteID(id string) bool { return strings.HasPrefix(id, ForgeNoteIDPrefix) }

// ReviewNoteIDPrefix marks the id of a note built at read time from a stored
// review document ("review:<review note id>:<n>"); like a forge comment it is
// never stored, so it is read-only.
const ReviewNoteIDPrefix = "review:"

// IsReviewNoteID reports whether id names a note of a review document.
func IsReviewNoteID(id string) bool { return strings.HasPrefix(id, ReviewNoteIDPrefix) }

// IsReadOnlyNoteID reports whether id names a note built at read time — a
// forge comment or a review's note — that no note mutation may touch.
func IsReadOnlyNoteID(id string) bool { return IsForgeNoteID(id) || IsReviewNoteID(id) }

// NoteTagResolved tags a forge thread its reviewers marked resolved.
const NoteTagResolved = "resolved"

// NoteHasTag reports whether n carries tag.
func NoteHasTag(n Note, tag string) bool {
	for _, t := range n.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// NoteSide is the diff side a note's line range indexes.
type NoteSide string

const (
	NoteSideOld NoteSide = "old"
	NoteSideNew NoteSide = "new"
)

// NoteStatus is a note's resolution against the diff it is drawn over. It is
// COMPUTED at open/refresh time and never stored: active (found where it was,
// or found again elsewhere), stale (the anchored text is gone but the file is
// not), orphaned (the file/commit itself is gone).
type NoteStatus string

const (
	NoteActive   NoteStatus = "active"
	NoteStale    NoteStatus = "stale"
	NoteOrphaned NoteStatus = "orphaned"
)

// Note is one review note: machine-local working material anchored to a range
// of lines on one side of one file, at one address. It is engine-free (plain
// data, like Bookmark) and persisted by internal/notes as TOML.
//
// A reply (ParentID != "") inherits its parent's Address, Side, Range and
// ContextHash at creation time and is re-anchored with the parent.
type Note struct {
	ID          string      `toml:"id"`
	ParentID    string      `toml:"parent_id,omitempty"`
	Source      NoteSource  `toml:"source"`
	Author      string      `toml:"author,omitempty"`
	Address     FileAddress `toml:"address"`
	Side        NoteSide    `toml:"side"`
	Range       [2]int      `toml:"range"` // 1-based, inclusive
	ContextHash string      `toml:"context_hash"`
	Summary     string      `toml:"summary"`
	Rationale   string      `toml:"rationale,omitempty"`
	Tags        []string    `toml:"tags,omitempty"`
	Confidence  float64     `toml:"confidence,omitempty"`
	// Scope is the reviewed range (hex "a..b") of a review note; "" otherwise.
	Scope string `toml:"scope,omitempty"`
	// Preview names the scope a note was written in, when it was written in
	// one: a merge preview "<target>...<source>" by branch NAMES, a commit
	// pair "<a7>..<b7>". Such a note is stored on the tip like any commit
	// note; this is how the tip's Files view can say where it came from.
	Preview string    `toml:"preview,omitempty"`
	Created time.Time `toml:"created"`
	Updated time.Time `toml:"updated"`
}

// IsReply reports whether n hangs off another note.
func (n Note) IsReply() bool { return n.ParentID != "" }

// ReviewTag marks a commit-level note that holds an AI review.
const ReviewTag = "review"

// IsCommitLevel reports a note about a whole commit: a commit and no path.
func (n Note) IsCommitLevel() bool {
	return n.Address.State == StateCommitted && n.Address.Commit != "" && n.Address.Path == ""
}

// IsShelfLevel reports a note about a whole shelf entry: an entry id and no
// path. gg writes one when a set cannot carry what it annotates (the
// deletions and renames of a recycled worktree).
func (n Note) IsShelfLevel() bool {
	return n.Address.State == StateShelf && n.Address.ShelfID != "" && n.Address.Path == ""
}

// IsEntryLevel reports a note about a whole object — a commit or a shelf
// entry. It has no line to re-anchor.
func (n Note) IsEntryLevel() bool { return n.IsCommitLevel() || n.IsShelfLevel() }

// IsReviewNote reports a commit-level note tagged ReviewTag. It is the ONLY
// test for "this is an AI review": Address.Branch alone proves nothing (the
// TUI fills it on working-tree line notes too).
func (n Note) IsReviewNote() bool { return n.IsCommitLevel() && NoteHasTag(n, ReviewTag) }

// NoteContextHash fingerprints the anchored lines: each line trimmed of
// leading/trailing whitespace, joined with "\n" (no trailing newline), hex
// sha256. Trimming makes re-indentation a non-event; the join makes phase-2
// multi-line hunk anchors reuse this function unchanged.
func NoteContextHash(lines []string) string {
	trimmed := make([]string, len(lines))
	for i, l := range lines {
		trimmed[i] = strings.TrimSpace(l)
	}
	sum := sha256.Sum256([]byte(strings.Join(trimmed, "\n")))
	return hex.EncodeToString(sum[:])
}
