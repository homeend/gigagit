// Package model holds shared git data types used across the engine and frontends.
package model

import "time"

// FileKind classifies a changed path.
type FileKind int

const (
	KindTracked FileKind = iota
	KindUntracked
	KindIgnored
	KindUnmerged
)

// FileStatus is one entry from `git status --porcelain=v2`.
// Staged/Unstaged hold the porcelain XY status bytes ('.' = unmodified).
type FileStatus struct {
	Path     string
	OrigPath string // populated for renames/copies
	Staged   byte
	Unstaged byte
	Kind     FileKind
}

// StashEntry is one stash list row: its ref (stash@{N}) and human description.
type StashEntry struct {
	Ref     string // "stash@{0}"
	Subject string // text after the ref, e.g. "On main: WIP on main"
}

// WorkingTreeStatus is a snapshot of the working tree and branch position.
type WorkingTreeStatus struct {
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
	Files    []FileStatus
}

// Counts summarises a WorkingTreeStatus.
type Counts struct {
	Staged     int
	Unstaged   int
	Untracked  int
	Conflicted int
}

// Counts tallies file states. A conflicted (unmerged) file is counted only as
// Conflicted, never as Staged/Unstaged.
func (w WorkingTreeStatus) Counts() Counts {
	var c Counts
	for _, f := range w.Files {
		switch f.Kind {
		case KindUntracked:
			c.Untracked++
		case KindUnmerged:
			c.Conflicted++
		default:
			if f.Staged != '.' && f.Staged != 0 {
				c.Staged++
			}
			if f.Unstaged != '.' && f.Unstaged != 0 {
				c.Unstaged++
			}
		}
	}
	return c
}

// Branch is a local branch ref.
type Branch struct {
	Name     string
	Upstream string
	Ahead    int
	Behind   int
	IsHead   bool
	Hash     string
	UnixTime int64 // committer time (unix seconds) of the branch tip; 0 if unknown
}

// RemoteBranch is one entry from `git for-each-ref refs/remotes`.
type RemoteBranch struct {
	Name     string // short ref, e.g. "origin/feature/x"
	Remote   string // "origin"
	Branch   string // "feature/x" (Name with the remote prefix removed)
	Hash     string // short object name
	UnixTime int64  // committer time (unix seconds); 0 if unknown
}

// Worktree is one entry from `git worktree list --porcelain`.
type Worktree struct {
	Path     string
	Branch   string // short branch name, "" if detached/bare
	Head     string
	Detached bool
	Bare     bool
}

// Commit is one entry from the commit log.
type Commit struct {
	Hash     string
	Parents  []string
	Author   string
	Subject  string
	UnixTime int64
}

// CommitFile is one changed path within a commit.
type CommitFile struct {
	Status  string // single letter: A M D R C T (score stripped from R/C)
	Path    string // new path
	OldPath string // set only for renames/copies
}

// FileSource identifies where a FileRef's bytes come from.
type FileSource int

const (
	SourceUnstaged FileSource = iota // working-tree file
	SourceStaged                     // index version
	SourceCommit                     // file at a commit/branch (Locator = rev)
	SourceShelf                      // a shelf entry (Locator = entry id)
)

// FileRef names a file located somewhere resolvable to bytes. It is the shared
// address behind "compare anything" and "copy a file anywhere as unstaged".
type FileRef struct {
	Source  FileSource
	Locator string // commit rev for SourceCommit; entry id for SourceShelf; "" otherwise
	Path    string // repo-relative path (origin path for a shelf entry)
}

// ShelfBucket is a named collection of shelf entries. The "default" bucket is
// implicit; Hidden buckets are gg-internal and excluded from normal listing.
type ShelfBucket struct {
	Name   string
	Hidden bool
}

// ShelfEntry is one shelved file: immutable content plus provenance metadata.
type ShelfEntry struct {
	ID      string // "<source>-<pathslug>-<shorthash>"
	Bucket  string
	Source  string // human-readable origin: "unstaged" | "staged" | "<rev>"
	Path    string // origin repo-relative path
	SHA     string // content hash; also the blob filename
	Size    int64
	Created time.Time
}

// BookmarkState is where in its git lifecycle a bookmarked file was taken from.
type BookmarkState int

const (
	StateCommitted BookmarkState = iota // a commit/branch file (permanent → SHA)
	StateShelf                          // a shelf entry (permanent → SHA)
	StateStaged                         // a worktree's index file (live)
	StateUnstaged                       // a worktree's working file, tracked-modified (live)
	StateUntracked                      // a worktree's working file, new (live)
)

// String renders the state word used in a bookmark's display string.
func (s BookmarkState) String() string {
	switch s {
	case StateCommitted:
		return "commit"
	case StateShelf:
		return "shelf"
	case StateStaged:
		return "staged"
	case StateUntracked:
		return "untracked"
	default:
		return "unstaged"
	}
}

// Bookmark is a richly-addressed reference to a file. The address fields are the
// identity and the display; SHA is the content determinator for permanent states
// (committed/shelf) only — "" means fetch live by the address.
type Bookmark struct {
	Worktree string // worktree top-level (staged/unstaged/untracked); "" otherwise
	Branch   string // branch name when known; "" otherwise
	Commit   string // commit sha (committed); "" otherwise
	ShelfID  string // shelf entry id (shelf); "" otherwise
	Path     string // path within the tree/worktree
	State    BookmarkState
	SHA      string // blob checksum; set ⇔ permanent
	ID       string // derived from the address
	Label    string // human label; defaults to the display string
	Created  time.Time
}
