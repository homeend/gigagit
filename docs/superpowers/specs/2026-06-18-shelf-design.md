# Shelf — design

**Date:** 2026-06-18
**Status:** approved (pending spec review)
**Feature 1 of 2** in the "shelf + bookmarks" feature set (Shelf first; Bookmarks
is a separate later spec).

## Purpose

Let a user copy a file's *bytes* — from the working tree, the index, or any
commit — into a named, persistent, **non-git** store ("the shelf"), and later
write those bytes back into the working tree as unstaged changes or compare them
against any other version of a file.

The shelf is deliberately **not** `git stash`: a shelved file is a frozen,
content-addressed copy that survives even if the source file is **permanently
deleted** from the repository. That safety is the whole reason it is non-git.

This feature also lands a small shared foundation — a uniform `FileRef`
("a file located somewhere, resolvable to bytes") plus two domain primitives —
that the later **Bookmarks** feature reuses, and that makes "compare anything
with anything" and "copy a file anywhere as unstaged" fall out for near-free
because the diff engine already accepts two arbitrary byte sources.

## Scope of this slice

- TUI **and** CLI ship together.
- Sources you can shelve from: **unstaged** (working tree), **staged** (index),
  and **any commit**. (Worktree-of-another-checkout and stash are deferred — the
  `FileRef.Source` enum leaves room for them.)
- Compare is a **TUI** capability this slice: shelf-entry ↔ current working-tree
  version, and shelf-entry ↔ shelf-entry. Arbitrary cross-surface comparison
  (a commit's file ↔ a branch's file, etc.) is **Bookmarks'** job, not built here.
- CLI is `add` / `restore` / `list` / `rm` only — no CLI diff this slice.

## Non-goals

- Not a replacement for `git stash` (which stashes a whole working-tree
  changeset through git). Shelf is per-file and non-git.
- No syncing the shelf across machines; it is machine-local state.
- No automatic/implicit shelving (e.g. "shelf before discard") in this slice —
  though the store's hidden-bucket support leaves room for gg-internal use later.

## Terminology

The single term is **shelf** everywhere — feature name, package, CLI namespace,
TUI tab, menu copy. There is no "shelve"/"unshelve" verb. The restore-to-worktree
action is **restore**.

---

## 1. The `FileRef` foundation

A new plain type in `internal/model` — a uniform address for "a file somewhere":

```go
// FileRef names a file located somewhere resolvable to bytes.
type FileRef struct {
    Source  FileSource // Unstaged | Staged | Commit | Shelf
    Locator string     // commit hash for Commit; entry id for Shelf; "" otherwise
    Path    string     // repo-relative path (origin path for a shelf entry)
}

type FileSource int

const (
    SourceUnstaged FileSource = iota // working-tree file
    SourceStaged                     // index version
    SourceCommit                     // file at a commit/branch (Locator = rev)
    SourceShelf                      // a shelf entry (Locator = entry id)
)
```

Two domain primitives are built on it. **These are the "ultimate goal" of the
whole feature set**, and most of the work is already done by existing code:

- `ResolveBytes(ctx, ref) ([]byte, error)` — dispatches by `Source`:
  - `Unstaged` → existing `WorktreeFile(path)`
  - `Staged`   → `ShowFile(ctx, ":"+path)` (index blob; `:path` git syntax)
  - `Commit`   → existing `ShowFile(ctx, rev, path)`
  - `Shelf`    → `shelf.Store.Get(entryID)`
  - Comparing any two refs = resolve both → feed the **existing** `Differ`
    (which already takes two lazy `ByteSource`s). No new diff code.

- `WriteWorktreeFile(ctx, path, data) error` — writes bytes into the working
  tree as an unstaged change, under a **TreeWrite** reservation (so it cannot
  race a concurrent checkout). Backed by a new `git.Repo.WriteWorktreeFile`
  helper that joins `path` onto the worktree top-level and writes atomically
  (temp + rename), creating parent dirs as needed. This is "copy a file anywhere
  as unstaged".

`tui`/`cli` never call `ResolveBytes`/`WriteWorktreeFile` against `git` directly
— they go through `domain` (archtest-guarded).

---

## 2. The store — `shelf.Store` interface (the fixed API)

A new `internal/shelf` package. The **interface is the fixed API** the rest of
the system depends on; the backing implementation is swappable (a future bbolt
backend would satisfy the same interface without touching any consumer).

```go
package shelf

// Bucket is a named collection of entries. The "default" bucket is implicit.
// Hidden buckets are gg-internal and excluded from normal listing.
type Bucket struct {
    Name   string
    Hidden bool
}

// Entry is one shelved file: immutable bytes + provenance metadata.
type Entry struct {
    ID      string         // stable id, "<source>-<pathslug>-<shorthash>"
    Bucket  string
    Source  string         // human-readable origin: "unstaged" | "staged" | "<rev>"
    Path    string         // origin repo-relative path
    SHA     string         // content hash (also the blob filename)
    Size    int64
    Created time.Time
}

type Store interface {
    Put(bucket string, ref model.FileRef, data []byte) (Entry, error)
    Get(entryID string) ([]byte, error)
    List(bucket string, skip, limit int) ([]Entry, error) // paged; exhausted when len < limit
    Buckets() ([]Bucket, error)                            // few; unpaged
    Remove(entryID string) error
}
```

### Default implementation: content-addressed file store

Layout under the machine-local XDG state dir, **keyed by git common dir** (the
same keying the `repos` MRU registry uses, so a shelf belongs to a repository,
not a checkout, and survives the source file's permanent deletion):

```
$XDG_STATE_HOME/gg/shelf/<repo-key>/
  shelf.toml           # index: buckets -> entries (metadata only)
  blobs/
    <sha>              # immutable file content, named by content hash
```

- **`repo-key`** is a stable hash of the git common dir (mirrors how `repos`
  identifies a repo). Falls back gracefully (shelf disabled, clear message) when
  no home/state dir exists — same posture as `repos.toml`.
- **Blobs are immutable and content-addressed:** `Put` hashes the bytes, writes
  `blobs/<sha>` only if absent (atomic temp + rename), then records the entry in
  the index. Identical bytes shelved twice → **one blob** (dedup for free).
- **The index** (`shelf.toml`) is the only mutable file; every mutation rewrites
  it atomically (temp + rename), exactly like `repos.toml`. Last-writer-wins
  across processes is acceptable (shelf writes are rare and user-driven), the
  same tolerance `repos.toml` already accepts.
- **`Remove`** deletes the index entry and, if no other entry references the
  `sha`, deletes the blob — so disk is actually reclaimed (unlike a grow-only
  embedded DB file).
- **`List` paging** is offset/limit over the bucket's entries sorted by
  `Created` descending; caller detects exhaustion via `len(page) < limit`
  (mirrors `domain.logPage`).
- **Size cap:** `Put` refuses content larger than **10 MB** (`MaxShelfBytes`,
  mirroring the diff engine's `MaxDiffBytes`) with a clear error; nothing is
  written.
- **Default bucket:** `""` or `"default"` both address the implicit `default`
  bucket. **Hidden buckets** carry `Hidden: true` in the index and are omitted
  from `Buckets()`'s normal result / the TUI tab (reserved for future
  gg-internal use; no internal producer in this slice).

---

## 3. Layer wiring

```
internal/shelf  (pure store: interface + file impl; NO git, NO tui)
        |
internal/domain  owns a shelf.Store (like it owns the Differ); adds
        |        FileRef resolve/write primitives + shelf commands
   internal/git   gains Repo.WriteWorktreeFile + a staged-show path
        |
   tui / cli      call domain only (archtest-clean)
```

Shelf operations are **domain commands**, siblings of `StashList`/`StashCommit`
— **not** engine `Operation`s, because they have no mid-flight forks/decisions
to route through a `Decider`:

```go
func (s *Service) Shelf() ShelfService           // accessor, like Differ()/CommitFeed()

func (s *Service) ShelfAdd(ctx, ref model.FileRef, bucket string) (shelf.Entry, error)
func (s *Service) ShelfList(ctx, bucket string, skip, limit int) ([]shelf.Entry, error)
func (s *Service) ShelfBuckets(ctx) ([]shelf.Bucket, error)
func (s *Service) ShelfRestore(ctx, entryID, dest string, overwrite bool) error
func (s *Service) ShelfRemove(ctx, entryID string) error
```

- `ShelfAdd` resolves the ref's bytes under a **Read** reservation, then
  `Store.Put`. (No git mutation.)
- `ShelfRestore` reads the blob, then `WriteWorktreeFile(dest)` under a
  **TreeWrite** reservation. If `dest` exists and differs and `!overwrite` →
  returns a sentinel `ErrDestExists` the frontends turn into a confirm
  (TUI modal) or a `--force` hint (CLI exit 2).

---

## 4. TUI surface

### Shelf tab (browse)

A new **"Shelf" left-column tab**, the 4th in `leftTabs` alongside
Branches / Remotes / Worktrees; `ctrl+←/→` cycles to it. Backed by a paged
read-model mirroring `CommitFeed`: load an initial page, `LoadMore` on scroll.

- Rows render `[source] path  #shorthash` (e.g. `[unstaged] internal/tui/model.go  #a1b2`).
- A key cycles the **active bucket** (default first); hidden buckets are omitted.
  The bucket name shows in the tab/panel header.

### Add to shelf (capture)

A **`.`-menu action "Add to shelf"**, context-scoped so it appears wherever a
file row is focused: Files panel, Staged panel, commit-files list, the diff view,
and file history. The menu is the right home because all single keys with a
sensible mnemonic (`s`/`S`/`u`) are already taken and the `.` action menu exists
exactly for context actions.

- The action builds the appropriate `FileRef` for the focused surface
  (Files → `Unstaged`; Staged → `Staged`; commit-files / history → `Commit`
  with that commit's hash; diff view → the side under inspection) and calls
  `ShelfAdd(ref, "")` (default bucket).
- A second menu row **"Add to shelf (bucket…)"** opens a small text-input popup
  for a named bucket, then `ShelfAdd(ref, name)`.

### Restore / remove / compare (in the Shelf tab)

- **restore:** opens a path-input popup that starts **empty** (no default path —
  the user types a destination every time). On confirm, `ShelfRestore(entry,
  dest, false)`. If it returns `ErrDestExists`, raise an **Overwrite / Cancel**
  confirm modal (the existing pre-op `decisionState` pattern); Overwrite re-calls
  with `overwrite=true`. The entry is **kept** after restore.
- **remove:** `ShelfRemove(entry)`, with a confirm modal (destroys the frozen
  copy). A "clear bucket" variant removes all entries in the active bucket.
- **compare:** `enter` on an entry diffs it (Old) against the current
  working-tree version of its origin path (New) via the existing diff view;
  if the working-tree file is absent, diff against empty. Marking two entries
  (`m`) and a "compare marked" action diffs entry ↔ entry.

Exact row keys are chosen at plan time and advertised in **both** `help.go`
(the `?` pane) and the context-help **footer**, per project convention.

---

## 5. CLI surface

A new `gg shelf` namespace (dispatch on `args[0]`):

- `gg shelf add <path>...` — shelves the **unstaged** version of each path by
  default; `--staged` shelves the index version, `--rev <commit>` shelves that
  commit's version; `--bucket <name>` targets a bucket. Prints each new entry id.
- `gg shelf restore <entry> <dest>` — `<dest>` is a **required** positional;
  bare `restore <entry>` errors (exit 2). Writes the blob to `<dest>` as
  unstaged. `--force` overwrites an existing differing file (without it, an
  existing path → exit 2 with a `--force` hint). `--bucket` disambiguates if
  needed.
- `gg shelf list [--bucket <name>]` — prints entries (id, source, path, size,
  age), paged internally.
- `gg shelf rm <entry> [--bucket <name>]` — removes an entry.

Wiring gotcha (from `gg discard`): a new top-level command must be added to
**both** the `Run` dispatch switch **and** the `var commands` map in `cli.go`,
or `cmd/gg`'s `IsCommand` falls through and silently launches the TUI. An e2e
scenario guards this.

`internal/agentskill/using-gg.md` gains a `gg shelf` entry, `agentskill.Version`
bumps to **11**, and `gg init --update` regenerates the dogfood
`.claude/skills/using-gg/SKILL.md` (the `TestDogfoodSkillCopyInSync` guard).

---

## 6. Error handling

- **Oversize content** (> `MaxShelfBytes`): `Put` refuses, nothing written;
  TUI status message / CLI exit 2 with the size and cap.
- **Missing source** (ref resolves to nothing — e.g. path not in that commit):
  resolve returns an error; surfaced as a status message / CLI exit 1.
- **Restore onto an existing differing file** without consent: `ErrDestExists`
  → TUI Overwrite/Cancel modal / CLI exit 2 with `--force` hint.
- **No state dir** (no home): shelf is disabled with a clear message, mirroring
  `repos.toml`'s posture; the rest of gg is unaffected.
- **Corrupt/partial index:** atomic temp+rename writes mean a crash leaves the
  previous good `shelf.toml`; a blob without an index entry is harmless garbage
  reclaimable later.

## 7. Testing strategy

- **`internal/shelf`** (real temp dir): round-trip Put/Get; dedup (same bytes →
  one blob); size-cap refusal; paging (`List` exhaustion boundary); `Remove`
  reclaims an unreferenced blob but keeps a shared one; atomic-rewrite survives
  a simulated mid-write; hidden buckets excluded from `Buckets()`.
- **`internal/git`**: `WriteWorktreeFile` writes under the worktree top-level,
  creates parents, atomic; staged-show reads the index blob. `FakeRunner` argv
  assertions where a git invocation is involved; real-git temp repo for the
  worktree write.
- **`internal/domain`**: `ResolveBytes` dispatch per source (`FakeRunner` +
  a fake store); `ShelfRestore` `ErrDestExists` path; reservation modes.
- **`internal/tui`**: Shelf tab renders a populated bucket; `.`-menu shows
  "Add to shelf" only where a file is focused; restore popup + Overwrite modal
  flow; help/footer coverage drift guard.
- **`internal/cli`**: `add`/`restore`/`list`/`rm` unit tests incl. required-dest
  and `--force` gates; both-registries dispatch check.
- **e2e** (`e2e/scenarios/`): `shelf add` a file → **delete the file** from the
  repo → `shelf restore` it to a new path → assert content matches (proves the
  non-git "survives permanent deletion" property end-to-end).

## 8. Docs to update on completion

`CHANGELOG.md` (always), `README.md` (new TUI tab + CLI namespace),
`CLAUDE.md` package map (new `shelf` package; `FileRef` foundation),
`internal/agentskill/using-gg.md` (+ Version bump + `gg init --update`).

---

## Decisions locked during brainstorming

1. Two features total; **Shelf first**, Bookmarks a separate later spec.
   Bookmarks are live **pointers** that may dangle; to preserve something you
   shelf it (frozen copy). Clean separation.
2. Named **buckets** with an implicit **default**; some buckets may be
   **hidden** (gg-internal). Entry identity is derived `source + path + hash`.
3. **TUI + CLI** ship together.
4. Storage is the **file-store + TOML index**, behind a **fixed `Store`
   interface** so the backend (e.g. bbolt) is swappable.
5. `List` is **paged**, not list-all.
6. The single term is **shelf** everywhere; the restore-to-worktree verb is
   **restore**.
7. **Restore always requires an explicit destination path** — required CLI
   positional; the TUI popup starts **empty with no default**. Overwrite needs
   explicit consent.
