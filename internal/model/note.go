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
)

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
	Created     time.Time   `toml:"created"`
	Updated     time.Time   `toml:"updated"`
}

// IsReply reports whether n hangs off another note.
func (n Note) IsReply() bool { return n.ParentID != "" }

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
