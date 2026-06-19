# Bookmarks — design

**Date:** 2026-06-19
**Status:** approved (pending spec review)
**Feature 2 of 2** in the "shelf + bookmarks" feature set. Builds on the shared
foundation shipped with **Shelf** (`docs/superpowers/specs/2026-06-18-shelf-design.md`)
— the `domain.Differ` (compares two arbitrary byte sources) and the
`engine.WriteFile` op (writes bytes to the working tree with an Overwrite/Cancel
fork).

## Purpose

Let a user **bookmark a file located anywhere** in their git world — the working
tree, the index, any commit/branch, another worktree, a shelf entry — and later
**jump to it**, **compare two bookmarks**, or **paste its contents** into the
working tree as an unstaged change.

A bookmark is a **named reference with full provenance**, not a byte copy. Its
job is twofold, and these two jobs are kept strictly separate:

1. **Address** — *where the file came from*, captured as richly as possible
   (worktree, branch, commit, path, state). This is the bookmark's **identity**
   (what makes it a *specific* file) and its **display** (what the human reads to
   recognise it).
2. **Content determinator** — *how to know/fetch the bytes*. For content in
   **permanent storage** (a committed blob, a shelf blob) this is the **blob
   checksum (SHA)** — durable and content-addressed. For content **not** in
   permanent storage (staged / unstaged / untracked) there is no durable
   checksum, so the content *is* its location and is fetched **live** by the
   address.

The reason the two are separate: a checksum identifies *content*, never
*origin* — every empty `.gitignore` shares one SHA, and three `.gitignore`s at
three paths in one commit are three different files with the same checksum. So
the bookmark's identity is the **address**, and the SHA is only a content
attribute used for equality and durable fetch of permanent content.

This is the live-vs-frozen split with the **Shelf**: a committed/shelf file is
permanent so a bookmark to it is content-stable (frozen by SHA); a working-tree
or index file is a moving target so a bookmark to it re-reads **live**. To freeze
a *working* file's bytes against change/deletion, shelf it.

## Scope of this slice

- TUI **and** CLI ship together.
- Bookmark states (and their content determinators):
  - **committed** (a commit/branch file) → permanent → **SHA**
  - **shelf** (a shelf entry) → permanent → **SHA** (resolved from the shelf store)
  - **staged** (a worktree's index) → impermanent → **live** by `(worktree, path)`
  - **unstaged / untracked** (a worktree's working file) → impermanent → **live**
    by `(worktree, path)`
- Capabilities: **jump** (diff the bookmark's bytes against the current
  working-tree file at its path), **compare** two bookmarks, **paste** a
  bookmark's bytes to a chosen path, **remove** (with confirm).

## Non-goals

- No byte snapshotting of *working* files — that is the Shelf's job. A bookmark to
  a working/index file is a live reference; to freeze it, shelf it.
- No pre-computed dangling scan of the whole list (§4).
- No cross-machine sync; machine-local state, like the shelf and the repo registry.

## Terminology

The single term is **bookmark** everywhere — feature name, package, CLI
namespace, popup, menu copy. The write-to-working-tree verb is **paste**.

---

## 1. The bookmark record — address + content determinator

The shared plain type lives in `internal/model` (frontends render it through
their existing `model` import; they never import `internal/bookmark`,
archtest-guarded, like the shelf):

```go
// internal/model

type BookmarkState int
const (
	StateCommitted BookmarkState = iota // a commit/branch file (permanent)
	StateShelf                          // a shelf entry (permanent)
	StateStaged                         // a worktree's index file (live)
	StateUnstaged                       // a worktree's working file, tracked-modified (live)
	StateUntracked                      // a worktree's working file, new (live)
)

// Bookmark is a richly-addressed reference to a file. The ADDRESS fields are the
// identity (two bookmarks are the same iff their address matches) and the
// display; SHA is the content determinator for permanent states only.
type Bookmark struct {
	// --- address: capture as many coordinates as apply ---
	Worktree string // worktree top-level (staged/unstaged/untracked); "" otherwise
	Branch   string // branch name when known (committed via a branch ref); "" otherwise
	Commit   string // commit sha (committed); "" otherwise
	ShelfID  string // shelf entry id (shelf); "" otherwise
	Path     string // path within the tree/worktree
	State    BookmarkState

	// --- content determinator ---
	SHA string // blob checksum; SET ⇔ permanent (committed/shelf). "" ⇒ fetch live by address.

	// --- bookkeeping ---
	ID      string    // derived from the ADDRESS (stable, dedups same-place re-bookmarks)
	Label   string    // human label; defaults to the display string (§5), user-rename later
	Created time.Time
}
```

`ID` is a hash of the **address** (`State + Worktree + Branch + Commit + ShelfID
+ Path`), **not** the SHA — so the same empty `.gitignore` bookmarked from three
different paths/commits yields three distinct bookmarks, and re-bookmarking the
exact same place is idempotent.

## 2. Store — `internal/bookmark`, a fixed `Store` interface

A new `internal/bookmark` package holds **records, not bytes** — no blobs, no
content store. Behind a fixed interface (swappable backend, the user's "fixed
api" preference):

```go
package bookmark

type Store interface {
	Add(b model.Bookmark) (model.Bookmark, error)
	Get(id string) (model.Bookmark, error)
	List(skip, limit int) ([]model.Bookmark, error) // paged; exhausted when len < limit
	Remove(id string) error
}
```

Default impl: a single atomic-rewrite **`bookmarks.toml`** under
`$XDG_STATE/gg/bookmark/<repo-key>/` (keyed by git common dir; same keying and
temp+rename the shelf and `repos.toml` use). `List` pages by offset/limit over
entries sorted by `Created` desc. A missing/corrupt file acts as empty
(best-effort, like `repos.toml`).

## 3. Layer wiring + resolution

```
internal/bookmark  (pure store: interface + toml file impl; NO git, NO tui)
        |
internal/domain    owns a bookmark.Store (like the shelf.Store); adds the
        |          commands + the bookmark byte-resolver
   internal/git     gains 3 tiny one-invocation verbs (below)
        |
   tui / cli        call domain only (archtest-clean; internal/bookmark added to
                    the frontend forbidden-imports guard)
```

Domain commands (siblings of the shelf commands; no engine ops — no forks):

```go
func (s *Service) BookmarkAdd(ctx, b model.Bookmark) (model.Bookmark, error)
func (s *Service) BookmarkList(ctx, skip, limit int) ([]model.Bookmark, error)
func (s *Service) BookmarkGet(ctx, id string) (model.Bookmark, error)
func (s *Service) BookmarkRemove(ctx, id string) error
func (s *Service) BookmarkBytes(ctx, b model.Bookmark) ([]byte, error) // resolve to bytes
```

`BookmarkBytes` routes by `State` — **this is the only new resolution logic** (it
does NOT reuse `ResolveBytes`, because the SHA-fetch and worktree-qualified live
reads are new):

| State | fetch |
|---|---|
| `StateCommitted` | `git cat-file blob <SHA>` (durable; frozen content) |
| `StateShelf` | the shelf store, by `ShelfID` |
| `StateStaged` | `git -C <Worktree> show :<Path>` (live index of that worktree) |
| `StateUnstaged` / `StateUntracked` | read `<Worktree>/<Path>` from disk (live) |

Three new `git.Repo` verbs (each one `gitcmd`-built invocation):

```go
func (r *Repo) CatFileBlob(ctx, sha string) ([]byte, error)            // git cat-file blob <sha>
func (r *Repo) ShowFileInDir(ctx, dir, rev, path string) ([]byte, error) // git -C <dir> show <rev>:<path>
func (r *Repo) BlobSHA(ctx, rev, path string) (string, error)         // git rev-parse <rev>:<path>
```

`BlobSHA` is used at **capture** time to fill `SHA` for a committed file
(`git rev-parse <commit>:<path>`). `ShowFileInDir` with `rev == ""` reads a
named worktree's index (`-C` overrides cwd, so it works from the Service's
workdir). `CatFileBlob` resolves permanent content directly from its checksum.

**Compare** = `BookmarkBytes(a)` + `BookmarkBytes(b)` → the existing `Differ`.
**Paste** = `BookmarkBytes(b)` → `startOp(engine.WriteFile{...})`. Both reuse the
shipped machinery; only `BookmarkBytes` + the 3 verbs are new below the frontends.
A disabled store (no state dir) → `ErrBookmarksDisabled` (mirrors the shelf).

## 4. Dangling

A bookmark dangles when its target is gone, surfaced at **resolve** (jump /
compare / paste) — never crashes, never pre-scanned:

- **permanent (SHA)**: `git cat-file blob <SHA>` fails if the blob was gc'd (a
  bookmarked unreachable commit) or, for shelf, the entry was removed.
- **live (address)**: the worktree was removed, or the file deleted, or the path
  no longer staged.

The error shows in the status line (TUI) / non-zero exit (CLI). The list does
**not** pre-check every entry — resolving N targets (some spawning git) just to
paint a list is wasteful on a large repo, and a dangling target may resolve again
later (a branch re-fetched, a worktree re-added). A lazy "⚠" marker is a future
nicety, not v1.

## 5. TUI surface — capture + the quick-switcher popup

### Capture (`.`-menu "Bookmark this file")

Context-scoped exactly like "Add to shelf", wherever a single file is focused.
Each surface fills the address coordinates it knows and computes `SHA` only when
permanent:

| Focused surface | State | Address filled | SHA |
|---|---|---|---|
| **Files** panel (tracked-modified / new) | Unstaged / Untracked | Worktree = current top-level, Branch = current branch, Path | — (live) |
| **Staged** panel | Staged | Worktree = current top-level, Branch = current branch, Path | — (live) |
| commit **file tree** / **file history** | Committed | Commit = that commit sha, Path; Branch if known | `BlobSHA(commit, path)` |
| **Shelf** tab | Shelf | ShelfID, Path | the entry's SHA |

`Branch`/`Worktree` are recorded for **display/provenance** even when not needed
to fetch. `BookmarkAdd` derives `ID` from the address and defaults `Label` to the
display string (§5 below). To bookmark a file in *another* worktree, switch into
it first (its top-level is then captured).

### The switcher popup (opened by a key)

Bookmarks live in a **centered quick-switcher popup** (not a 5th left tab — avoids
tab crowding and the `B` collision with Branches), opened by a global key, a
type-to-filter list rendered through the existing popup/`renderWindow` machinery
(so `z` display-mode + filtering work). Each row shows the **display string**:

```
<container> / <commit-or-state> / <path>
```
e.g. `feat-x @ a1b2c3 / src/app.go`, `wt:hotfix / staged / src/app.go`,
`wt:hotfix / unstaged / src/app.go`, `shelf:default / README.md`. Built from the
address: container = branch or worktree (basename) or `shelf:<bucket>`; middle =
short commit / state word; then path.

In the popup:

- **`enter` = jump:** diff the bookmark's bytes (`BookmarkBytes`, Old) against the
  current working-tree file at its `Path` (New; empty if absent), in the existing
  diff view.
- **`m` + `m` = compare two bookmarks:** mark one, mark a second → diff the two
  resolved bookmarks (the shelf `Compare` pair-op pattern).
- **paste:** a key opens an **empty path-input popup** (mandatory destination, no
  default, like shelf restore); on submit, `BookmarkBytes` then
  `startOp(engine.WriteFile{Path: dest, Data: bytes})` (its Overwrite/Cancel fork
  is the standard modal).
- **remove:** a key → **confirmation modal** (`["Remove","Cancel"]`) →
  `BookmarkRemove`. Confirmed because re-locating and re-bookmarking the exact
  file in a large monorepo is real friction (the user's call).

Exact keys (popup-open, paste, remove) are chosen at plan time and advertised in
**both** `help.go` and the footer.

## 6. CLI surface — `gg bookmark`

Registered in **both** the `Run` switch **and** the `var commands` map in
`cli.go` (the gotcha the shelf/discard CLIs documented; an e2e guards it):

- `gg bookmark add [--rev <commit>] [--staged] [--worktree <path>] [--label <l>] <path>...`
  — stores a bookmark per path. Default = this worktree's **working** file
  (Unstaged, Worktree = workdir top-level). `--staged` = this worktree's index
  (Staged). `--worktree <path>` targets another worktree (combinable with
  `--staged`). `--rev <commit>` = a committed file (computes `SHA` via
  `BlobSHA`; mutually exclusive with `--staged`/`--worktree`). Prints each id.
- `gg bookmark list` — prints id, display string, path (paged internally).
- `gg bookmark rm <id>`.
- `gg bookmark paste [--force] <id> <dest>` — `<dest>` **required**; resolves the
  bookmark's bytes and writes them via `engine.WriteFile` (CLI policy decider:
  `--force`→Overwrite, else Cancel → exit 2 with a hint). Mirrors `gg shelf restore`.

`internal/agentskill/using-gg.md` gains a `gg bookmark` entry; `agentskill.Version`
bumps to **12**; `gg init --update` regenerates the dogfood SKILL.md.

## 7. Error handling

- **Disabled store** (no state dir): `ErrBookmarksDisabled`; the rest of gg is
  unaffected (mirrors the shelf).
- **Dangling** on jump/compare/paste: the underlying fetch error → status line /
  exit 1.
- **Paste onto an existing differing file:** `engine.WriteFile`'s Overwrite/Cancel
  `DecisionNeeded` → TUI modal / CLI `--force` policy.
- **Unknown id** (`rm`/`paste`): clear not-found → exit 2.

## 8. Testing strategy

- **`internal/git`**: `CatFileBlob` round-trips a known blob; `BlobSHA` returns
  `git rev-parse <commit>:<path>`; `ShowFileInDir` reads a *linked worktree's*
  index (real-git temp repo + linked worktree; stage differing content in the
  linked worktree and read it back).
- **`internal/bookmark`** (real temp dir): Add/Get/List round-trip; paging
  exhaustion; Remove; address-derived `ID` (same place → same id; same content at
  a different path → different id); missing-file acts as empty.
- **`internal/domain`**: `BookmarkBytes` per state — committed (cat-file by SHA),
  staged (worktree index, live), unstaged (disk, live), shelf (store); a
  partially-staged file proves staged ≠ unstaged bytes; disabled-store path.
- **`internal/tui`**: capture menu shows "Bookmark this file" per surface with the
  right state/address; switcher popup renders the display string + filters; jump
  opens a diff; the `m`+`m` Compare pair-op opens a two-bookmark diff; paste
  path-popup + Overwrite modal; remove confirm; help/footer drift guard.
- **`internal/cli`**: `add`/`list`/`rm`/`paste` incl. required-dest + `--force`
  gates + the flag→state mapping; both-registries dispatch check.
- **e2e** (`e2e/scenarios/`): the **live-pointer proof** — `bookmark add` a
  working-tree file at content "v1", **edit it to "v2"**, then `bookmark paste
  <id> <newpath>` and assert the pasted file is **"v2"** (live re-read), the
  observable inverse of the shelf's frozen-copy e2e.

## 9. Docs to update on completion

`CHANGELOG.md`, `README.md` (the switcher popup key + `.` capture + the
`gg bookmark` CLI), `CLAUDE.md` package map (new `bookmark` package; the 3 new
git verbs; the address-vs-checksum identity model), `internal/agentskill/using-gg.md`
(+ Version 12 + `gg init --update`).

---

## Decisions locked during brainstorming

1. A bookmark separates **address** (full provenance: worktree/branch/commit/
   shelf-id/path + state — the **identity** and the **display**) from a **content
   determinator** (the blob **SHA** for permanent content; **live-by-address** for
   working/index content). The checksum identifies content, never origin (empty
   `.gitignore`s collide), so identity is the address, not the SHA.
2. **Permanent → SHA (frozen):** a committed file and a shelf entry. **Impermanent
   → live-by-address:** staged, unstaged, untracked (a worktree's index/working
   file). This keeps the bookmark-vs-shelf line crisp: bookmark = live pointer to
   a moving target / durable reference to permanent content; shelf = frozen copy.
3. **TUI + CLI** ship together.
4. Bookmarks live in a **keyed quick-switcher popup**, NOT a 5th left tab.
5. **Remove confirms** (re-finding a file in a big monorepo is real friction).
6. The write-to-working-tree verb is **paste**, with a **mandatory** destination
   (required CLI positional; empty TUI popup).
7. Store is a tiny **`bookmarks.toml` record registry** behind a fixed
   `bookmark.Store` interface — no blobs.
8. Resolution adds only `BookmarkBytes` + 3 tiny git verbs (`CatFileBlob`,
   `ShowFileInDir`, `BlobSHA`); **compare and paste reuse the shipped `Differ`
   and `engine.WriteFile`** unchanged.
