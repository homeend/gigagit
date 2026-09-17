// Package model holds shared git data types used across the engine and frontends.
package model

import (
	"errors"
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

// RemoteHead is one branch on a remote as reported by `git ls-remote --heads`.
type RemoteHead struct {
	Name string // short branch name (no refs/heads/ prefix)
	Hash string // tip object id on the remote
}

// BranchVersion is one recorded pre-operation snapshot of a branch
// (refs/gg/versions/<branch>/<unix>-<op>).
type BranchVersion struct {
	Ref     string // full version ref
	Hash    string // snapshot tip (full sha)
	Subject string // tip commit subject
	Op      string // protocol op token: merge, rebase, restore, …
	Unix    int64  // when the snapshot was recorded

	// Endpoints recorded by the snapshot. Ours is the contribution frozen,
	// Other the tip it was landing on/against, Base their merge base. Empty for
	// a one-branch op (amend/reset/undo-commit/delete-branch/restore), which
	// records no preview.
	Ours, Other, Base string
	Source, Target    string
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

// CommitDateLayout is the one place gg writes the layout it displays a
// commit's date in. The TUI formats with it (the files-view header line and
// the commit-message popup); the web client re-implements it in JavaScript
// (files.js commitMetaLine) because the server ships unix seconds and the
// browser owns the viewer's timezone — internal/web/commitmetajs_test.go
// holds that port to THIS constant, so the browser and the terminal cannot
// drift into showing the same commit two different ways.
const CommitDateLayout = "2006-01-02 15:04"

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
	// EndpointInvalid is the ZERO VALUE, and it is deliberately not a usable
	// endpoint: an Endpoint{} is an unset variable or an error return, never
	// "the working tree". Every method panics on it rather than guessing.
	EndpointInvalid  EndpointKind = iota
	EndpointWorkTree              // the working tree (unstaged)
	EndpointIndex                 // the index (staged)
	EndpointCommit                // a commit, by Hash
	EndpointShelf                 // a shelved commit's frozen changed-file set, by ShelfID
	EndpointRef                   // a branch/tag TIP by name — unbounded, and it MOVES
	EndpointPair                  // a resolved <a>..<b> sha pair — bounded (a change-set)

	// endpointKindCount must stay LAST. It is the exhaustiveness bound:
	// endpoint_exhaustive_test.go walks EndpointInvalid+1 .. endpointKindCount
	// and fails for any kind with no table row, so adding a kind above this
	// line without adding a row breaks the build.
	endpointKindCount
)

// Endpoint names one side of a whole-tree comparison.
//
// Its fields are unexported on purpose: the only way to make one is a
// constructor (WorkTreeEndpoint, IndexEndpoint, CommitEndpoint,
// ShelfEndpoint, RefEndpoint, PairEndpoint), each of which validates. Holding
// an Endpoint therefore
// PROVES it is consistent, and no consumer re-checks. The zero value is
// EndpointInvalid -- an unset variable or an error return, never a usable
// endpoint.
type Endpoint struct {
	kind    EndpointKind
	hash    string // commit hash when kind == EndpointCommit; "" otherwise
	shelfID string // shelf entry id when kind == EndpointShelf; "" otherwise
	ref     string // branch/tag name when kind == EndpointRef; "" otherwise
	// a, b are the two RESOLVED shas of an EndpointPair. Deliberately shas and
	// not names, and deliberately no three-dot flag (plan 1b ruling R1): a
	// three-dot pair means merge-base(target, source)..source, and the merge
	// base is a RESOLUTION that only domain can make. Storing names here would
	// put a moving value in CacheTag, which is plan 1a's headline bug.
	a, b string
}

// Kind is the endpoint's kind. EndpointInvalid means unset.
func (e Endpoint) Kind() EndpointKind { return e.kind }

// Valid reports whether the endpoint came from a constructor.
func (e Endpoint) Valid() bool { return e.kind != EndpointInvalid }

// Hash is the commit id, or "" for any other kind.
func (e Endpoint) Hash() string { return e.hash }

// ShelfID is the shelf entry id, or "" for any other kind.
func (e Endpoint) ShelfID() string { return e.shelfID }

// Ref is the branch/tag name, or "" for any other kind.
func (e Endpoint) Ref() string { return e.ref }

// PairA and PairB are the older and newer sha of a pair endpoint, or "" for
// any other kind.
func (e Endpoint) PairA() string { return e.a }
func (e Endpoint) PairB() string { return e.b }

// Display is the human label for an endpoint.
func (e Endpoint) Display() string {
	switch e.kind {
	case EndpointWorkTree:
		return "Working Tree"
	case EndpointIndex:
		return "Staged"
	case EndpointShelf:
		id := e.shelfID
		if len(id) > 9 {
			id = id[:9]
		}
		return "shelf #" + id + " (frozen)"
	case EndpointCommit:
		return shortEndpointHash(e.hash)
	case EndpointRef:
		return e.ref
	case EndpointPair:
		return shortEndpointHash(e.a) + ".." + shortEndpointHash(e.b)
	default:
		panic(endpointKindBug("Display", e.kind))
	}
}

// shortEndpointHash is the 7-character display form of an object id, matching
// Display's commit arm.
func shortEndpointHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// FileRef maps the endpoint to a resolvable file reference for path.
func (e Endpoint) FileRef(path string) FileRef {
	switch e.kind {
	case EndpointWorkTree:
		return FileRef{Source: SourceUnstaged, Path: path}
	case EndpointIndex:
		return FileRef{Source: SourceStaged, Path: path}
	case EndpointShelf:
		return FileRef{Source: SourceShelf, Locator: e.shelfID, Path: path}
	case EndpointCommit:
		return FileRef{Source: SourceCommit, Locator: e.hash, Path: path}
	case EndpointRef:
		// `git show <ref>:<path>` is correct and is NOT a cache key, so a ref
		// is a legitimate file source even though CacheTag refuses it.
		return FileRef{Source: SourceCommit, Locator: e.ref, Path: path}
	case EndpointPair:
		// A pair's file content is its NEW side: the change-set's members are
		// read as they are at b. The older side is reached through the
		// comparison, not through a single FileRef.
		return FileRef{Source: SourceCommit, Locator: e.b, Path: path}
	default:
		panic(endpointKindBug("FileRef", e.kind))
	}
}

// IsLive reports whether the endpoint's content can change on disk (working
// tree or index) and therefore must never be cached.
func (e Endpoint) IsLive() bool {
	switch e.kind {
	case EndpointWorkTree, EndpointIndex:
		return true
	case EndpointCommit, EndpointShelf, EndpointPair:
		return false
	case EndpointRef:
		return true // a tip moves; nothing may cache a diff against it
	default:
		panic(endpointKindBug("IsLive", e.kind))
	}
}

// CacheTag is a stable cache-key fragment for the endpoint (only meaningful
// when !IsLive()).
func (e Endpoint) CacheTag() string {
	switch e.kind {
	case EndpointWorkTree:
		return "worktree"
	case EndpointIndex:
		return "index"
	case EndpointShelf:
		return "shelf:" + e.shelfID
	case EndpointCommit:
		return e.hash
	case EndpointRef:
		// NO SAFE ANSWER — see plan 1b ruling R3 and the cacheTagPanics column
		// in endpoint_exhaustive_test.go. Resolve the ref to a commit first.
		panic(endpointKindBug("CacheTag", e.kind))
	case EndpointPair:
		return "pair:" + e.a + ".." + e.b
	default:
		panic(endpointKindBug("CacheTag", e.kind))
	}
}

// endpointKindBug is the one panic message shape. An invalid kind is always a
// programming error -- an unset variable reaching a method, or a kind added to
// the iota block without teaching the methods about it -- so it names both the
// method and the kind.
func endpointKindBug(method string, k EndpointKind) string {
	if k == EndpointInvalid {
		return "model.Endpoint." + method + ": endpoint is unset (EndpointInvalid); it was never given a kind"
	}
	return fmt.Sprintf("model.Endpoint.%s: unknown EndpointKind %d; add a case arm and a row in endpoint_exhaustive_test.go", method, k)
}

// ErrEndpoint wraps every constructor refusal, so a caller can tell a
// malformed endpoint from a git failure without matching on prose.
var ErrEndpoint = errors.New("bad endpoint")

// WorkTreeEndpoint names the working tree. It cannot fail: there is nothing
// to validate.
func WorkTreeEndpoint() Endpoint { return Endpoint{kind: EndpointWorkTree} }

// IndexEndpoint names the index. It cannot fail.
func IndexEndpoint() Endpoint { return Endpoint{kind: EndpointIndex} }

// CommitEndpoint names the tree at a commit. hash must be 7..64 hex
// characters -- 64, not 40, because a sha-256 repository's commit ids are 64
// hex characters. The bound matches model.ParseLink's, so a link and an
// endpoint never disagree about what a commit id looks like.
func CommitEndpoint(hash string) (Endpoint, error) {
	if len(hash) < 7 || len(hash) > 64 {
		return Endpoint{}, fmt.Errorf("%w: commit hash must be 7..64 characters, got %d", ErrEndpoint, len(hash))
	}
	for i := 0; i < len(hash); i++ {
		if !isHexDigit(hash[i]) {
			return Endpoint{}, fmt.Errorf("%w: commit hash must be hex, got %q", ErrEndpoint, hash)
		}
	}
	return Endpoint{kind: EndpointCommit, hash: hash}, nil
}

// ShelfEndpoint names a shelved commit's frozen changed-file set.
func ShelfEndpoint(id string) (Endpoint, error) {
	if id == "" {
		return Endpoint{}, fmt.Errorf("%w: shelf id is required", ErrEndpoint)
	}
	return Endpoint{kind: EndpointShelf, shelfID: id}, nil
}

// RefEndpoint names a branch or tag TIP. It is UNBOUNDED — a point, the whole
// tree at that tip — and it MOVES, so domain resolves it to a commit endpoint
// before anything compares or caches against it (plan 1b ruling R2).
func RefEndpoint(name string) (Endpoint, error) {
	if !LinkRefOK(name) {
		return Endpoint{}, fmt.Errorf("%w: %q is not a branch or tag name", ErrEndpoint, name)
	}
	return Endpoint{kind: EndpointRef, ref: name}, nil
}

// PairEndpoint names a CHANGE-SET: the files that differ between two resolved
// commits, a → b, older → newer. Both halves must already be full object ids —
// see the field comment on Endpoint.a for why names are refused here.
//
// a == b is LEGAL and means the empty change-set (ruling R7): a fully merged
// branch's three-dot pair has merge-base(target, source) == source, and an
// empty bounded set is a result, not an error (spec §6).
func PairEndpoint(a, b string) (Endpoint, error) {
	if !commitHashOK(a) {
		return Endpoint{}, fmt.Errorf("%w: pair's older side %q is not a commit id", ErrEndpoint, a)
	}
	if !commitHashOK(b) {
		return Endpoint{}, fmt.Errorf("%w: pair's newer side %q is not a commit id", ErrEndpoint, b)
	}
	return Endpoint{kind: EndpointPair, a: a, b: b}, nil
}

// commitHashOK reports whether hash is a plausible commit object id: 7..64
// hex characters -- 64, not 40, because a sha-256 repository's commit ids are
// 64 hex characters. The bound matches model.ParseLink's and CommitEndpoint's,
// so a link, a commit endpoint and a pair endpoint never disagree about what a
// commit id looks like. CommitEndpoint keeps its own inline check (its two
// branches report a more specific error than a single bool would allow);
// PairEndpoint shares this helper for both of its halves.
func commitHashOK(hash string) bool {
	if len(hash) < 7 || len(hash) > 64 {
		return false
	}
	for i := 0; i < len(hash); i++ {
		if !isHexDigit(hash[i]) {
			return false
		}
	}
	return true
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// Bounded reports whether the endpoint evaluates to a FINITE, enumerated set
// of paths rather than every file in the repository (spec section 3.1). A
// shelf entry carries its own member list; a tree, a tip and the index do not.
//
// DELIBERATE DEVIATION FROM THE SPEC. Section 4.0 sketches a stored `bounded
// bool` field, "computed ONCE at construction, never re-derived". A switch on
// the kind is used instead, because boundedness is a total function of the
// kind alone -- for every kind in this plan AND for the two 1b adds
// (EndpointRef is unbounded, EndpointPair is bounded). A stored field would
// duplicate the kind and introduce a second thing that can disagree with it.
// The spec's actual requirement -- that the rule live in exactly one place and
// no consumer re-derive it -- is met by this one switch, pinned by
// TestBoundedMatchesTheSpecRule. If 1b finds a kind whose boundedness is NOT
// determined by the kind, revisit this.
func (e Endpoint) Bounded() bool {
	switch e.kind {
	case EndpointShelf, EndpointPair:
		return true
	case EndpointWorkTree, EndpointIndex, EndpointCommit, EndpointRef:
		return false
	default:
		panic(endpointKindBug("Bounded", e.kind))
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
