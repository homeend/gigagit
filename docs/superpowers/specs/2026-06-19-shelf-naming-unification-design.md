# Shelf naming unification — design

**Date:** 2026-06-19
**Status:** approved (brainstorm)
**Scope:** `internal/model`, `internal/shelf`, `internal/domain`, `internal/tui`,
`internal/cli` (+ docs/agentskill). One cohesive feature → one plan/worktree/merge.

## Problem

Shelf entries are hard to read. A `ShelfEntry` records only a bare `Source`
string (`unstaged` | `staged` | `<rev>`) plus `Path` + `SHA`, and renders
`[source] path #sha8` (TUI `shelfRows()`) / `id<TAB>source<TAB>path<TAB>size`
(`gg shelf list`). Bookmarks, by contrast, capture a full structured **address**
(worktree / branch / commit / state) and render a clear
`<container> / <state-or-commit> / <path>` via `bookmarkDisplay`. The shelf
should read the same way so it's obvious *what was shelved and from where* — and
the display logic should be shared, not duplicated.

## Decisions (locked in brainstorm)

1. **Enrich at capture** — record the structured provenance (worktree, branch,
   commit, state) on each shelf entry at shelve-time, mirroring how bookmarks
   capture their address. (Not a cosmetic reformat of existing data.)
2. **Shared `model.FileAddress` + `Display()`** — one small value type in the
   `model` package owns the address shape and its display string. The shelf
   *stores* it; `Bookmark` gains an `Address()` method that builds one (no change
   to `Bookmark`'s struct or its literals).
3. **No backward compatibility** — there is no shelf data in use. The
   `ShelfEntry` schema changes cleanly; no migration, fallback, or legacy display.

## Design

### Shared address type (`internal/model`)

- **Rename the state enum `BookmarkState` → `FileState`** (type name only). The
  constants `StateCommitted`, `StateShelf`, `StateStaged`, `StateUnstaged`,
  `StateUntracked` and the `String()` method keep their names. `Bookmark.State`'s
  field type changes to `FileState`. The enum is now shared by shelf + bookmark,
  so the bookmark-specific type name is wrong.

- **New `model.FileAddress`**:
  ```go
  type FileAddress struct {
      Worktree string // working/index/untracked states; "" otherwise
      Branch   string // branch name when known
      Commit   string // commit sha/rev (StateCommitted)
      ShelfID  string // shelf entry id (StateShelf)
      Path     string // path within the tree/worktree
      State    FileState
  }
  ```
  - `func (a FileAddress) Display() string` — the generalized `bookmarkDisplay`:
    - container: `StateCommitted` → `Branch` (or `"commit"` when blank);
      `StateShelf` → `"shelf"`; working states → `"wt:" + filepath.Base(Worktree)`.
    - middle: `StateCommitted` with `len(Commit) >= 7` → `Commit[:7]`; else
      `State.String()`.
    - result: `"<container> / <middle> / <Path>"`.
  - `func (a FileAddress) FileRef() FileRef` — maps the address to the existing
    byte-resolution ref: `StateUnstaged`/`StateUntracked` → `SourceUnstaged`;
    `StateStaged` → `SourceStaged`; `StateCommitted` → `SourceCommit` with
    `Locator = Commit`; `StateShelf` → `SourceShelf` with `Locator = ShelfID`.
    Path carries through. (Byte resolution stays against the service repo;
    `Worktree`/`Branch` are display-only provenance.)

- **`func (b Bookmark) Address() FileAddress`** — builds a `FileAddress` from the
  bookmark's existing fields. `Bookmark`'s struct is unchanged.

### `ShelfEntry` schema (`internal/model`)

Replace the bare `Source string` + `Path string` with the structured origin:
```go
type ShelfEntry struct {
    ID      string
    Bucket  string
    Origin  FileAddress // provenance + display (was: Source string, Path string)
    SHA     string      // content hash; also the blob filename
    Size    int64
    Created time.Time
}
```
ID stays `<source-word>-<slug(Origin.Path)>-<sha8>`, with a small
`idSource(Origin)` helper deriving the word: `StateCommitted` → `Origin.Commit`,
`StateStaged` → `"staged"`, else `"unstaged"` (preserves the old ID semantics
from `sourceLabel`).

### Capture path (`internal/shelf`, `internal/domain`)

- `shelf.FileStore.Put(bucket string, addr model.FileAddress, data []byte)
  (model.ShelfEntry, error)` — was `Put(bucket, ref FileRef, data)`. Stores
  `Origin = addr`; derives the ID via `idSource(addr)` + `slug(addr.Path)` +
  `sha[:8]`.
- `domain.Service.ShelfAdd(ctx, addr model.FileAddress, bucket string)` — was
  `ShelfAdd(ctx, ref FileRef, bucket)`. Resolves bytes via
  `s.ResolveBytes(ctx, addr.FileRef())`, then `store.Put(bucket, addr, data)`.

### Frontends

- **TUI** (`internal/tui/shelf.go`):
  - Delete `focusedShelfRef`. Shelf capture reuses **`focusedBookmark()`**: take
    its `model.Bookmark`, reject `StateShelf` (can't re-shelf a shelf entry),
    else pass `b.Address()` to `ShelfAdd`. `shelfAddCmd` takes a
    `model.FileAddress`.
  - `shelfRows()` renders `e.Origin.Display()` (drop the `#sha8` suffix — the
    bookmark display has none).
  - `bookmarkDisplay(b)` becomes `b.Address().Display()` (thin wrapper or inline).
- **CLI** (`internal/cli/shelf.go`):
  - `gg shelf add` builds a `model.FileAddress` mirroring `gg bookmark add`:
    `--staged` → `StateStaged`; `--rev <c>` → `StateCommitted, Commit=c`; default
    → `StateUnstaged`; `--worktree`/`TopLevel()` fills `Worktree`,
    `CurrentBranch()` fills `Branch` for working states. Passes it to `ShelfAdd`.
  - `gg shelf list` prints `id<TAB>Origin.Display()<TAB>sizeB`.

`gg shelf restore`/compare are unaffected — they key off the entry ID
(`FileRef{Source: SourceShelf, Locator: id}`).

## Testing

- **model:** `FileAddress.Display()` per state (committed/shelf/working);
  `FileAddress.FileRef()` mapping per state; `Bookmark.Address()` round-trip;
  `FileState.String()`.
- **shelf store:** `Put` stores `Origin` and derives the ID; round-trip via
  `List`/`Get`.
- **domain:** `ShelfAdd(addr)` resolves the right bytes and stores `Origin`;
  `ShelfList` returns entries carrying `Origin`.
- **tui:** `shelfRows()` shows `Origin.Display()`; shelf-add is **absent** on the
  Shelf tab (StateShelf rejected); shelf-add present + correct address on Files /
  commit-tree surfaces (via `focusedBookmark`).
- **cli:** `gg shelf add` builds the right address; `gg shelf list` output shows
  the display string.
- Update existing shelf/bookmark tests for the `BookmarkState`→`FileState` rename
  and the `ShelfEntry` struct change.

## Docs

- `CHANGELOG.md` (always).
- `README.md` — `gg shelf list` output format changed; TUI Shelf rows reworded.
- `CLAUDE.md` — package-map note that `model.FileAddress` is the shared
  address/display behind both shelf provenance and bookmarks.
- `internal/agentskill/using-gg.md` + bump `agentskill.Version`, then
  `gg init --update` — `gg shelf add` flags + `gg shelf list` output changed.
