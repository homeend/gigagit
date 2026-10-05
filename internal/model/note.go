package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
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

// ParseReviewNoteID splits a review remark id "review:<id>:<n>".
func ParseReviewNoteID(id string) (reviewID string, n int, ok bool) {
	rest, found := strings.CutPrefix(id, ReviewNoteIDPrefix)
	if !found {
		return "", 0, false
	}
	i := strings.LastIndexByte(rest, ':')
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil || n < 0 {
		return "", 0, false
	}
	return rest[:i], n, true
}

// StoredRootID is the STORED note a thread root id lives under: the review
// for a review remark ("review:<id>:<n>" → "<id>"), the id itself otherwise.
// A reply to a remark is a stored child of its review: the store's orphan
// prune, cap and cascade all key on this.
func StoredRootID(id string) string {
	if rid, _, ok := ParseReviewNoteID(id); ok {
		return rid
	}
	return id
}

// ThreadResolution marks one note thread resolved (GitHub's "Resolve
// conversation"). Root is the thread's root id — a stored note id or a
// review remark id; RemarkFP is set for a remark root (it follows the remark
// when the review is re-saved). It is stored beside the notes, in the
// root's part, and goes when the root goes.
type ThreadResolution struct {
	Root     string    `toml:"root"`
	By       string    `toml:"by,omitempty"`
	At       time.Time `toml:"at"`
	RemarkFP string    `toml:"remark_fp,omitempty"`
}

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
	Preview string `toml:"preview,omitempty"`
	// PreviewBranch is the branch a commit pair's note (a range review) was
	// written on: such a review is shown on that branch and no other
	// (domain.ReviewShownOn). Empty for a merge preview's note — a preview
	// review belongs to its preview, not to a branch.
	PreviewBranch string `toml:"preview_branch,omitempty"`
	// PreviewBase is where a merge preview's range began when the note was
	// written (the merge base, full sha): the range still opens from it once
	// the branch was merged and git can no longer tell.
	PreviewBase string `toml:"preview_base,omitempty"`
	// Files is a working-changes review's fingerprint: every file the review
	// read, with git's blob id of the bytes it read (spec 2026-10-04 working
	// reviews §4). Empty on every other note.
	Files []NoteFile `toml:"files,omitempty"`
	// Link is an address the note points at — a gg:// link or a full commit
	// sha (a reply's "here is the fix"). Empty on most notes.
	Link string `toml:"link,omitempty"`
	// Remark, RemarkFP and RemarkSummary are set only on a reply to a review
	// remark, whose ParentID is the REVIEW's note id (so an older gg's
	// orphan prune keeps it): Remark is the remark id it answered
	// ("review:<id>:<n>" when written), RemarkFP the remark's fingerprint,
	// which finds it again when the review is re-saved, RemarkSummary its
	// summary, which an outdated thread still shows.
	Remark        string    `toml:"remark,omitempty"`
	RemarkFP      string    `toml:"remark_fp,omitempty"`
	RemarkSummary string    `toml:"remark_summary,omitempty"`
	Created       time.Time `toml:"created"`
	Updated       time.Time `toml:"updated"`
}

// IsReply reports whether n hangs off another note.
func (n Note) IsReply() bool { return n.ParentID != "" }

// StoredParent is the stored note n hangs off: the review for a reply to a
// review remark, ParentID otherwise ("" for a root).
func (n Note) StoredParent() string { return StoredRootID(n.ParentID) }

// IsRemarkReply reports a reply to a review remark: it is shown only inside
// its review, never as a note of its own.
func (n Note) IsRemarkReply() bool { return n.IsReply() && n.Remark != "" }

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

// NoteFile is one file a working-changes review read: its repo-relative
// path and git's blob id of the working-tree bytes it reviewed (hex, in the
// repo's object format). Deleted: the review saw the file absent.
type NoteFile struct {
	Path    string `toml:"path"`
	Blob    string `toml:"blob,omitempty"`
	Deleted bool   `toml:"deleted,omitempty"`
}

// IsWorktreeLevel reports a note about a whole worktree's uncommitted
// changes: a live address with a worktree and no path, commit or shelf entry.
func (n Note) IsWorktreeLevel() bool {
	a := n.Address
	return a.Path == "" && a.Commit == "" && a.ShelfID == "" && a.Worktree != "" &&
		(a.State == StateUnstaged || a.State == StateStaged || a.State == StateUntracked)
}

// IsWorkingReview reports a worktree-level note tagged ReviewTag: an AI
// review of uncommitted changes. IsReviewNote stays commit-only; the review
// read model checks both.
func (n Note) IsWorkingReview() bool { return n.IsWorktreeLevel() && NoteHasTag(n, ReviewTag) }

// IsEntryLevel reports a note about a whole object — a commit, a shelf entry
// or a worktree's uncommitted changes. It has no line to re-anchor.
func (n Note) IsEntryLevel() bool {
	return n.IsCommitLevel() || n.IsShelfLevel() || n.IsWorktreeLevel()
}

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
