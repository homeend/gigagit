# Bookmarks — design

**Date:** 2026-06-19
**Status:** approved (pending spec review)
**Feature 2 of 2** in the "shelf + bookmarks" feature set. Builds directly on the
shared foundation shipped with **Shelf** (`docs/superpowers/specs/2026-06-18-shelf-design.md`).

## Purpose

Let a user **bookmark a file located anywhere** — the working tree, the index,
any commit/branch, another worktree's live working tree, or a shelf entry — and
later **jump to it**, **compare two bookmarks**, or **paste its current contents**
into the working tree as an unstaged change.

A bookmark is a **live pointer**, not a copy: it stores *where* a file is
(`model.FileRef` + a label), and resolving it re-reads the target's **current**
bytes. Bookmark an unstaged file, edit it, then paste — you get the *edited*
content. This is the defining difference from the **Shelf**, whose entries are
frozen byte copies. The division of labour the user settled during brainstorming:
**bookmark = live pointer (may dangle); to preserve bytes against deletion, shelf
it.**

Because the foundation already exists, almost nothing new is needed below the
frontends: `model.FileRef` already addresses Unstaged/Staged/Commit/Shelf,
`domain.ResolveBytes` resolves any ref to bytes, the `domain.Differ` already
compares two arbitrary byte sources, and `engine.WriteFile` writes bytes to the
working tree with an Overwrite/Cancel fork. Bookmarks add: one new `FileRef`
source (Worktree), a tiny persistent pointer store, domain commands, a TUI
quick-switcher popup + capture menu, and a `gg bookmark` CLI.

## Scope of this slice

- TUI **and** CLI ship together.
- Bookmark sources: a **working-tree file** (captured as `SourceWorktree` so the
  bookmark records *which* worktree — see §1/§5; this is how "bookmark files in
  different worktrees" works), a **Commit/branch** file, and a **Shelf** entry (a
  bookmark captured on the Shelf tab — supported because `FileRef` already has
  it). The transient `SourceUnstaged`/`SourceStaged` sources (this-worktree
  working file / index) are **not** bookmark sources: they name no specific
  worktree, so they make poor persistent pointers; a working-tree file is
  bookmarked as `SourceWorktree` instead.
- Capabilities: **jump** (diff the bookmark's live bytes against the current
  working-tree file at its path), **compare** two bookmarks, **paste** a
  bookmark's live bytes to a chosen path, **remove** (with confirm).

## Non-goals

- No byte snapshotting — that is the Shelf's job. A bookmark never freezes
  content; if you want a copy that survives deletion, shelf it.
- No pre-computed dangling scan of the whole list (see §4).
- No cross-machine sync; machine-local state, like the shelf and the repo registry.

## Terminology

The single term is **bookmark** everywhere — feature name, package, CLI
namespace, popup, menu copy. The jump-to-working-tree verb is **paste**.

---

## 1. `FileRef` extension — `SourceWorktree`

Add one source to `model.FileSource`:

```go
const (
	SourceUnstaged FileSource = iota // working-tree file (THIS worktree)
	SourceStaged                     // index version
	SourceCommit                     // file at a commit/branch (Locator = rev)
	SourceShelf                      // a shelf entry (Locator = entry id)
	SourceWorktree                   // another worktree's live file (Locator = worktree path)
)
```

`domain.ResolveBytes` gains a `SourceWorktree` branch that reads the file from
the *named* worktree's working tree: `os.ReadFile(filepath.Join(Locator, Path))`,
where `Locator` is the worktree's absolute top-level and `Path` is repo-relative
(mirroring how `ReadWorktreeFile` joins a top-level). A missing worktree or file
returns an error → the bookmark **dangles** (§4).

Why a working-tree file is `SourceWorktree`, not `SourceUnstaged`: a persistent
bookmark must name *which* worktree it points into. `SourceUnstaged` means "this
worktree" and would silently re-point when the user switches worktrees;
`SourceWorktree(<abs worktree path>, <path>)` is stable and names the target
unambiguously — which is exactly what makes "bookmark files in different
worktrees" work (you bookmark a working-tree file from whichever worktree you are
in, and each bookmark remembers its worktree). The capturing frontend fills
`Locator` from the current worktree's top-level (`m.currentWorktree` / `TopLevel`).

`model.FileRef` itself is unchanged in shape (`{Source, Locator, Path}`); only
the enum grows and the resolver gains a case.

## 2. Store — `internal/bookmark`, a fixed `Store` interface

A new `internal/bookmark` package holds **pointers, not bytes** — so unlike the
shelf there are no blobs, no content addressing, no dedup. The plain types live
in `internal/model` (frontends never import `internal/bookmark`, archtest-guarded,
exactly like the shelf):

```go
// internal/model
type Bookmark struct {
	ID      string    // stable: "<source>-<pathslug>-<shorthash>"
	Label   string    // human label; defaults to "<source>:<path>", user-editable later
	Source  FileSource
	Locator string    // commit rev / worktree path / shelf id / "" (matches FileRef)
	Path    string    // repo-relative path
	Created time.Time
}
```

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
`$XDG_STATE/gg/bookmark/<repo-key>/` (keyed by git common dir, the same keying
and atomic temp+rename the shelf and `repos.toml` use). Pointers are tiny, so the
whole registry is one TOML array; no content store. `List` pages by offset/limit
over the entries sorted by `Created` desc. A `FileRef()` helper converts a
`Bookmark` to a `model.FileRef` for resolution. The fixed interface keeps a
future backend swappable (the user's "fixed api" preference).

## 3. Layer wiring (foundation reused wholesale)

```
internal/bookmark  (pure store: interface + toml file impl; NO git, NO tui)
        |
internal/domain    owns a bookmark.Store (like the shelf.Store); adds commands
        |          + the SourceWorktree resolve branch in ResolveBytes
   tui / cli        call domain only (archtest-clean; internal/bookmark added to
                    the frontend forbidden-imports guard)
```

Domain commands, siblings of the shelf commands (no engine ops — no forks):

```go
func (s *Service) BookmarkAdd(ctx, ref model.FileRef, label string) (model.Bookmark, error)
func (s *Service) BookmarkList(ctx, skip, limit int) ([]model.Bookmark, error)
func (s *Service) BookmarkGet(ctx, id string) (model.Bookmark, error)
func (s *Service) BookmarkRemove(ctx, id string) error
```

- `BookmarkAdd` stores the pointer (no resolve — capture is cheap and must not
  fail if the target is briefly unreadable). Default label `"<source>:<path>"`.
- **Jump / compare / paste resolve through the EXISTING `ResolveBytes`** (which a
  bookmark feeds via `bookmark.FileRef()`), the existing `Differ`, and the
  existing `engine.WriteFile` op — so this feature adds **zero** new engine code
  and no new diff/write paths. A disabled store (no state dir) →
  `ErrBookmarksDisabled`, mirroring `ErrShelfDisabled`.

## 4. Dangling

A bookmark whose target is gone — file deleted, commit unreachable, worktree
removed, shelf entry purged — fails at **resolve time**: `ResolveBytes` returns an
error. This surfaces **on access**:

- **jump / compare / paste** of a dangling bookmark → the error shows in the
  status line (TUI) / non-zero exit with the message (CLI). Nothing crashes.
- The **list does not pre-check** every entry (resolving N targets — some
  spawning `git show` — just to paint the list would be wasteful on a large repo,
  and a bookmark that dangles today may resolve tomorrow when a branch is
  re-fetched). A lazy "⚠ dangling" marker is a deliberate future nicety, not v1.

## 5. TUI surface — the bookmark quick-switcher popup

### Capture (everywhere a file is focused)

A **`.`-menu action "Bookmark this file"**, context-scoped exactly like "Add to
shelf", available wherever a single file is focused — the **Files** panel (→
`SourceWorktree` with the current worktree's top-level as `Locator`), a **commit's
file tree** / **file history** (→ `SourceCommit` with that commit's hash), and the
**Shelf tab** (→ `SourceShelf`). It reuses the same focused-file discipline the
shelf capture uses, mapping each surface to the right bookmark source. To bookmark
a file in *another* worktree, switch into that worktree first (the bookmark then
records *its* top-level, so it still resolves after switching back).
`BookmarkAdd(ref, "")` → default label `"<source>:<path>"`.

### The switcher popup (opened by a key)

Bookmarks live in a **centered popup quick-switcher** (not a left tab — avoids a
5th cycling tab and the `B` letter collision with Branches), opened by a global
key. It is a type-to-filter list rendered through the existing popup/`renderWindow`
machinery (so `z` display-mode + filtering work like every other list popup).
Rows: `<label>   [<source>] <path>`.

In the popup:

- **`enter` = jump:** diff the bookmark's **live** bytes (Old, via `ResolveBytes`)
  against the current working-tree file at its `Path` (New; empty if absent),
  using the existing diff view — the same construction shelf compare uses.
- **`m` + `m` = compare two bookmarks:** mark one, mark a second → diff the two
  resolved bookmarks (reuses the shelf pair-op pattern: a `Compare` pair-op).
- **paste:** a key opens an **empty path-input popup** (mandatory destination, no
  default — same rule as shelf restore); on submit, `ResolveBytes` the bookmark
  then `startOp(engine.WriteFile{Path: dest, Data: bytes})`, whose Overwrite/Cancel
  fork is the standard modal.
- **remove:** a key → **confirmation modal** (`decisionState`, `["Remove","Cancel"]`)
  → `BookmarkRemove`. Confirmed because re-locating and re-bookmarking the exact
  file in a large monorepo is real friction (the user's call).

Exact keys (popup-open, paste, remove) are chosen at plan time and advertised in
**both** `help.go` and the context-help footer, per project convention.

## 6. CLI surface — `gg bookmark`

A new `gg bookmark` namespace (dispatch on `args[0]`), registered in **both** the
`Run` switch **and** the `var commands` map in `cli.go` (the gotcha the shelf/
discard CLIs documented; an e2e guards it):

- `gg bookmark add [--rev <commit> | --worktree <path>] [--label <l>] <path>...`
  — stores a pointer per path. Default source is **this worktree**
  (`SourceWorktree` with the CLI's workdir top-level as `Locator`); `--rev`
  bookmarks a commit/branch file; `--worktree <path>` bookmarks a file in another
  worktree explicitly. Prints each id.
- `gg bookmark list` — prints id, label, source, path (paged internally).
- `gg bookmark rm <id>` — removes a bookmark.
- `gg bookmark paste [--force] <id> <dest>` — `<dest>` is a **required** positional;
  resolves the bookmark's live bytes and writes them to `<dest>` as unstaged via
  `engine.WriteFile` (the CLI policy decider maps `--force`→Overwrite, else
  Cancel → exit 2 with a hint). Mirrors `gg shelf restore`.

`internal/agentskill/using-gg.md` gains a `gg bookmark` entry; `agentskill.Version`
bumps to **12**; `gg init --update` regenerates the dogfood SKILL.md.

## 7. Error handling

- **Disabled store** (no state dir): `ErrBookmarksDisabled`, mirroring the shelf;
  the rest of gg is unaffected.
- **Dangling target** on jump/compare/paste: the underlying `ResolveBytes` error
  → TUI status line / CLI exit 1.
- **Paste onto an existing differing file:** the `engine.WriteFile` Overwrite/Cancel
  `DecisionNeeded` → TUI modal / CLI `--force` policy.
- **Unknown id** (`rm`/`paste`): a clear not-found error / exit 2.

## 8. Testing strategy

- **`internal/bookmark`** (real temp dir): Add/Get/List round-trip; paging
  exhaustion boundary; Remove; atomic-rewrite survives a simulated mid-write;
  missing/corrupt file acts as empty (best-effort, like `repos.toml`).
- **`internal/domain`**: `ResolveBytes` `SourceWorktree` reads the named
  worktree's file (real-git temp repo with a linked worktree); `BookmarkAdd`
  round-trips through a fake store; disabled-store error path.
- **`internal/tui`**: capture menu shows "Bookmark this file" wherever a file is
  focused (incl. the Worktrees tab) and is absent otherwise; the switcher popup
  renders + filters; `enter` opens a diff; the `m`+`m` Compare pair-op opens a
  two-bookmark diff; paste path-popup + Overwrite modal; remove confirm; help/
  footer coverage drift guard.
- **`internal/cli`**: `add`/`list`/`rm`/`paste` unit tests incl. required-dest +
  `--force` gates; both-registries dispatch check.
- **e2e** (`e2e/scenarios/`): the **pointer-semantics proof** — `bookmark add` a
  working-tree file at content "v1" (a `SourceWorktree` pointer), **edit it to
  "v2"**, then `bookmark paste <id> <newpath>` and assert the pasted file is
  **"v2"** (live re-read), not "v1". This is the observable inverse of the shelf's
  frozen-copy e2e (where the copy stayed "v1" after the working tree changed).

## 9. Docs to update on completion

`CHANGELOG.md` (always), `README.md` (the switcher popup key + `.` capture + the
`gg bookmark` CLI), `CLAUDE.md` package map (new `bookmark` package; the
`SourceWorktree` addition), `internal/agentskill/using-gg.md` (+ Version 12 +
`gg init --update`).

---

## Decisions locked during brainstorming

1. Bookmarks are **live pointers** (resolve re-reads the target's current bytes)
   and may **dangle**; to preserve bytes against deletion, shelf instead. Clean
   split from the [[shelf-feature]].
2. A new **`SourceWorktree`** `FileRef` source (Locator = worktree path) lets a
   bookmark target another worktree's live working-tree file.
3. **TUI + CLI** ship together.
4. Bookmarks live in a **keyed popup quick-switcher**, NOT a 5th left tab (avoids
   tab crowding and the `B` letter collision; matches the "switch to a bookmarked
   file" mental model).
5. **Remove confirms** (re-finding and re-bookmarking a file in a big monorepo is
   real friction — the same destructive-action friction the shelf remove spends).
6. The single term is **bookmark**; the write-to-worktree verb is **paste**, with
   a **mandatory** destination (required CLI positional; empty TUI popup).
7. Store is a tiny **`bookmarks.toml` pointer registry** behind a fixed
   `bookmark.Store` interface — no blobs (that is the shelf's job).
8. Jump / compare / paste reuse the existing `ResolveBytes` + `Differ` +
   `engine.WriteFile` — **no new engine or diff code**.
