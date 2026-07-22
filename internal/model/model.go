// Package model holds shared git data types used across the engine and frontends.
package model

import (
	"fmt"
	"path/filepath"
	"time"
)

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

// RefInfo is one row from a generic `git for-each-ref` read.
type RefInfo struct {
	Ref     string // full ref name
	Hash    string // full object id
	Subject string // commit subject
}

// BranchVersion is one recorded pre-operation snapshot of a branch
// (refs/gg/versions/<branch>/<unix>-<op>).
type BranchVersion struct {
	Ref     string // full version ref
	Hash    string // snapshot tip (full sha)
	Subject string // tip commit subject
	Op      string // protocol op token: merge, rebase, restore, …
	Unix    int64  // when the snapshot was recorded
}

// VersionedBranch summarizes one branch's recorded versions.
type VersionedBranch struct {
	Branch     string
	Deleted    bool // branch no longer exists in refs/heads
	Count      int
	LatestUnix int64
}

// RemoteBranch is one entry from `git for-each-ref refs/remotes`.
type RemoteBranch struct {
	Name     string // short ref, e.g. "origin/feature/x"
	Remote   string // "origin"
	Branch   string // "feature/x" (Name with the remote prefix removed)
	Hash     string // short object name
	UnixTime int64  // committer time (unix seconds); 0 if unknown
}

// Tag is one git tag (refs/tags). Target is the commit the tag resolves to (the
// peeled commit for an annotated tag, the direct commit for a lightweight one).
// Subject is the annotated tag's message subject, or — for a lightweight tag —
// its target commit's subject.
type Tag struct {
	Name      string
	Target    string
	Annotated bool
	Subject   string
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
	Refs     []Ref  // ref decorations (branch/tag/HEAD); nil when undecorated
	Source   string // branch the commit was reached from in the walk (%S); "" when unknown
}

// LogLine is one terse history row (short sha + subject) — the gg log /
// gg show header unit.
type LogLine struct {
	Hash    string // short sha (%h)
	Subject string
}

// DiffSpec addresses a diff: working tree (zero value), the index
// (Cached), a commit or range (Rev), optionally narrowed to Paths.
type DiffSpec struct {
	Cached bool
	Rev    string // "", a commit-ish, or a range string (A..B / A...B)
	Paths  []string
}

// DiffStat is one file's terse change stat (from git --numstat).
type DiffStat struct {
	Path    string
	OldPath string // non-empty for renames; Path is then the new name
	Added   int
	Deleted int
	Binary  bool
}

// ReflogEntry is one HEAD reflog record (git reflog), newest first.
type ReflogEntry struct {
	Selector  string // "HEAD@{0}"
	Hash      string // full SHA
	ShortHash string // abbreviated SHA
	Subject   string // %gs, e.g. "commit: add foo" or "checkout: moving from main to dev"
	Rel       string // relative time, e.g. "2 hours ago" (from %gd under --date=relative)
}

// RefKind classifies a ref decoration on a commit.
type RefKind int

const (
	RefLocal  RefKind = iota // local branch
	RefRemote                // remote-tracking branch
	RefTag
	RefHead // detached HEAD marker
)

// Ref is one ref decoration pointing at a commit (from `git log %D`). Head marks
// the local branch that HEAD currently points at (the current branch).
type Ref struct {
	Name string
	Kind RefKind
	Head bool
}

// CommitFile is one changed path within a commit.
type CommitFile struct {
	Status  string // single letter: A M D R C T (score stripped from R/C)
	Path    string // new path
	OldPath string // set only for renames/copies
}

// CompareOrigins attributes changed paths to each side of a branch
// comparison: APaths/BPaths hold every path the respective branch touched
// since the two diverged (diff merge-base..tip), keyed for membership tests.
// Renames contribute both their old and new path.
type CompareOrigins struct {
	APaths map[string]bool
	BPaths map[string]bool
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

// EndpointKind names the kind of a comparison Endpoint.
type EndpointKind int

const (
	EndpointWorkTree EndpointKind = iota // the working tree (unstaged)
	EndpointIndex                        // the index (staged)
	EndpointCommit                       // a commit, by Hash
)

// Endpoint names one side of a whole-tree comparison.
type Endpoint struct {
	Kind EndpointKind
	Hash string // commit hash when Kind == EndpointCommit; "" otherwise
}

// Display is the human label for an endpoint.
func (e Endpoint) Display() string {
	switch e.Kind {
	case EndpointWorkTree:
		return "Working Tree"
	case EndpointIndex:
		return "Staged"
	default:
		if len(e.Hash) > 7 {
			return e.Hash[:7]
		}
		return e.Hash
	}
}

// FileRef maps the endpoint to a resolvable file reference for path.
func (e Endpoint) FileRef(path string) FileRef {
	switch e.Kind {
	case EndpointWorkTree:
		return FileRef{Source: SourceUnstaged, Path: path}
	case EndpointIndex:
		return FileRef{Source: SourceStaged, Path: path}
	default:
		return FileRef{Source: SourceCommit, Locator: e.Hash, Path: path}
	}
}

// IsLive reports whether the endpoint's content can change on disk (working
// tree or index) and therefore must never be cached.
func (e Endpoint) IsLive() bool {
	return e.Kind == EndpointWorkTree || e.Kind == EndpointIndex
}

// CacheTag is a stable cache-key fragment for the endpoint (only meaningful
// when !IsLive()).
func (e Endpoint) CacheTag() string {
	switch e.Kind {
	case EndpointWorkTree:
		return "worktree"
	case EndpointIndex:
		return "index"
	default:
		return e.Hash
	}
}

// ShelfKind distinguishes a shelf entry's blob payload. A file entry's blob is
// raw file bytes; a commit entry's blob is a tar archive of the commit's
// changed files (extracted on copy-out). The kind is stored, never inferred.
type ShelfKind int

const (
	ShelfKindFile   ShelfKind = iota // blob = raw file bytes (default)
	ShelfKindCommit                  // blob = tar of the commit's changed files
)

// ShelfBucket is a named collection of shelf entries. The "default" bucket is
// implicit; Hidden buckets are gg-internal and excluded from normal listing.
type ShelfBucket struct {
	Name   string
	Hidden bool
}

// ShelfEntry is one shelved file: immutable content plus structured provenance.
type ShelfEntry struct {
	ID     string // "<source-word>-<pathslug>-<shorthash>"
	Bucket string
	Kind   ShelfKind   // file (raw bytes) vs commit (tar archive)
	Origin FileAddress // where it was captured from (provenance + display)
	Label  string      // human name (commit entries); "" = none. Display-only, not in ID.
	SHA    string      // content hash; also the blob filename
	Size   int64
	// PatchSHA/PatchSize describe an optional second blob for a commit entry:
	// the commit's format-patch mailbox, snapshotted at shelve time so the
	// entry can be re-applied as a commit (git am) even after the commit
	// object is gc'd. "" = none (a file entry, an old entry, a merge commit,
	// or an oversized/failed patch).
	PatchSHA  string
	PatchSize int64
	Created   time.Time
}

// IsCommit reports whether the entry is a shelved commit (tar payload) rather
// than a single file (raw bytes).
func (e ShelfEntry) IsCommit() bool { return e.Kind == ShelfKindCommit }

// ExportFile is one file to write during a copy-to-temp-dir export: a
// repo-relative path plus its bytes. Produced by domain, consumed by
// engine.ExportToDir.
type ExportFile struct {
	RelPath string
	Data    []byte
}

// FileState is where in its git lifecycle a referenced file was taken from.
// Shared by a bookmark's address and a shelf entry's origin.
type FileState int

const (
	StateCommitted FileState = iota // a commit/branch file (permanent → SHA)
	StateShelf                      // a shelf entry (permanent → SHA)
	StateStaged                     // a worktree's index file (live)
	StateUnstaged                   // a worktree's working file, tracked-modified (live)
	StateUntracked                  // a worktree's working file, new (live)
)

// String renders the state word used in an address's display string.
func (s FileState) String() string {
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
	State    FileState
	SHA      string // blob checksum; set ⇔ permanent
	ID       string // derived from the address
	Label    string // human label; defaults to the display string
	Created  time.Time
}

// FileAddress is the shared, structured provenance of a file: the identity AND
// the human display behind both a bookmark's address and a shelf entry's origin.
type FileAddress struct {
	Worktree string // working/index/untracked states; "" otherwise
	Branch   string // branch name when known
	Commit   string // commit sha/rev (StateCommitted)
	ShelfID  string // shelf entry id (StateShelf)
	Path     string // path within the tree/worktree
	State    FileState
}

// Display renders "<container> / <state-or-commit> / <path>".
func (a FileAddress) Display() string {
	container := "?"
	switch a.State {
	case StateCommitted:
		container = a.Branch
		if container == "" {
			container = "commit"
		}
	case StateShelf:
		container = "shelf"
	default:
		container = "wt:" + filepath.Base(a.Worktree)
	}
	mid := a.State.String()
	if a.State == StateCommitted && len(a.Commit) >= 7 {
		mid = a.Commit[:7]
	}
	if a.Path == "" {
		return fmt.Sprintf("%s / %s", container, mid)
	}
	return fmt.Sprintf("%s / %s / %s", container, mid, a.Path)
}

// FileRef maps the address to the byte-resolution ref used by ResolveBytes.
// Byte resolution stays against the service repo; Worktree/Branch are
// display-only provenance.
func (a FileAddress) FileRef() FileRef {
	switch a.State {
	case StateStaged:
		return FileRef{Source: SourceStaged, Path: a.Path}
	case StateCommitted:
		return FileRef{Source: SourceCommit, Locator: a.Commit, Path: a.Path}
	case StateShelf:
		return FileRef{Source: SourceShelf, Locator: a.ShelfID, Path: a.Path}
	default: // StateUnstaged, StateUntracked
		return FileRef{Source: SourceUnstaged, Path: a.Path}
	}
}

// IsCommit reports whether the bookmark points at a commit itself (a path-less
// committed pointer) rather than a file within a commit.
func (b Bookmark) IsCommit() bool {
	return b.Path == "" && b.State == StateCommitted
}

// Address builds the FileAddress a bookmark points at.
func (b Bookmark) Address() FileAddress {
	return FileAddress{
		Worktree: b.Worktree, Branch: b.Branch, Commit: b.Commit,
		ShelfID: b.ShelfID, Path: b.Path, State: b.State,
	}
}
