# Recycle a worktree: the `shelve` answer (+ shelf-entry notes) — design

**Date:** 2026-09-28 · **Branch:** `feat/recycle-shelve` · **Status:** spec, awaiting review
**Builds on:** `2026-09-28-recycle-worktree-design.md` (its "Follow-ups" named this one)

## Goal

When the worktree being recycled has uncommitted work, let the user park it
on the shelf instead of committing or throwing it away. Everything the
worktree changed becomes ONE shelf file set named `WIP on <leaving branch>`;
the worktree is then cleaned and switched.

A shelf file set holds file contents only, so it cannot say "this file was
deleted" or "this file was renamed". That information goes into a **note on
the shelf entry** — a new kind of note this feature adds first.

Two parts, built in this order on one branch:

1. **Shelf-entry notes** — a note about a whole shelf entry (no file, no
   lines). gg writes them; users read them.
2. **The `shelve` answer** of the `recycle.dirty` decision.

## Part 1 — shelf-entry notes

### Rulings (user, 2026-09-28)

- Scope is minimal: model + store + domain + CLI read + overview, **plus** a
  marker and a read-only viewer in the TUI and web shelf lists.
- **No user-facing creation.** A shelf-entry note is only created when gg
  must annotate an entry (today: a recycle-shelve with deletions/renames).
  Users see and read it; they do not add, edit or reply to it.

### Model

A shelf-level note is an ordinary `model.Note` whose address is
`FileAddress{State: StateShelf, ShelfID: <entry id>, Path: ""}`, `Range`
`[0,0]`, empty `ContextHash`, `Side` `new`.

- `Note.IsShelfLevel()` — `State == StateShelf && ShelfID != "" && Path == ""`.
- `Note.IsEntryLevel()` — `IsCommitLevel() || IsShelfLevel()`: a note about a
  whole object, with no line to re-anchor.

### Store (`internal/notes`)

- `capOldestFirst` exempts **entry-level** roots (and their replies) instead
  of commit-level only: an annotation is never dropped to make room.

### Domain

- `NoteAdd` accepts a shelf-level address: the range/context check is
  skipped (there are no lines), the entry must exist (error
  `shelf entry <id> not found` otherwise).
- `ShelfNotes(ctx, id) ([]model.Note, error)` — the entry's shelf-level roots,
  oldest first (replies not expected but tolerated).
- `NoteCounts` gains `ByShelf map[string]int` (shelf-level threads per entry).
  Per-file shelf notes keep their current handling.
- `ShelfRemove` also removes the entry's shelf-level notes (best effort: a
  notes failure does not resurrect the entry, it is logged).
- Sweep: shelf-level notes are never aged out and never re-anchored. One is
  dropped only when its shelf entry no longer exists (the store is local, the
  check is cheap and cannot be "unreadable").
- `NotesOverview`: `NoteShelfNotes` gains `Entry []ResolvedNote` (status
  always active); `Count` includes them; the shelf group is listed when it
  has entry notes and/or file notes.

### TUI

- Shelf popup rows whose entry has `ByShelf > 0` show the note marker `◆`
  (the glyph the file badges use) after the label.
- Key **`n`** on a shelf row (the `G` switcher is key-driven, it has no `.`
  menu) opens the note viewer; the `[n] note` hint shows only when the
  selected entry has a note. It opens a read-only content popup: per note the summary line,
  then author · date, then the text (`Rationale`) wrapped. Scrolls; `ctrl+t`
  maximizes (layer-stack popup); esc returns to the shelf popup.
- The all-notes overview lists shelf-entry notes under their entry.
- All strings through `i18n.T`, four bundles.

### Web

- `/api/shelf` entries carry `notes` (count).
- The sidebar shelf row shows `◆` when `notes > 0`.
- The shelf entry menu gets **"View note"** → read-only dialog with the same
  content (`GET /api/shelf/notes?id=<id>`).

### CLI

- `gg note list --shelf <id>` prints the entry's notes (the usual list
  format). No `add --shelf`.

## Part 2 — the `shelve` answer

### What the user sees

The `recycle.dirty` modal gains a row:

```
<path> has uncommitted changes on <branch>
  commit     …
  shelve     Put everything on the shelf as "WIP on <branch>", then clean
  discard    …
  abort
```

Option values (protocol, English): `commit`, `shelve`, `discard`, `abort`, in
that order. CLI: `gg worktree recycle --on-dirty=shelve <path> <branch>`.

Success summary: `recycled <old> → <new> in <path>; shelved as "WIP on <old>"`.

### Flow inside `RecycleWorktree` (answer = `shelve`)

1. `wt.StageAll` in the target (`git add -A`): staged, unstaged and untracked
   changes all land in the index. Ignored files are untouched (as discard).
2. `deps.shelveStaged(ctx, target, old)` — a new injected seam (below). It
   returns the stored entry.
3. The existing discard (reset --hard HEAD + clean `:/` without `-x`).
4. The existing switch.

### The seam: `OpDeps.ShelveStaged`

```go
// ShelveStaged freezes the INDEX of the worktree at dir (every path that
// differs from HEAD) into one shelf file set labelled "WIP on <branch>", and
// annotates it with a shelf-level note when deletions or renames cannot be
// carried by the set. Nil = ErrNoShelve.
ShelveStaged func(ctx context.Context, dir, branch string) (model.ShelfEntry, error)
```

Wired by `domain.Execute` (like `RepoAt`), implemented in domain as
`shelveStagedIn(ctx, dir, branch)`:

- Lists `git -C <dir> diff --cached --name-status -M -z HEAD` (the existing
  `DiffTreeFiles(commit HEAD → index)` verb on `repo.InDir(dir)`).
- A/M/T/C and the new side of R: content = `git -C <dir> show :<path>` (the
  index blob). **These reads go straight to the `InDir` repo, never through a
  domain query**: queries take a Read reservation and the op already holds
  TreeWrite on the same gate — they would deadlock.
- D: recorded, no content. R: new path shelved, `old → new` recorded.
- Zero content paths (every change was a deletion) → the set gets ONE empty
  placeholder member `delete.me` (a set cannot be empty — the `.gitkeep`
  idea). No collision is possible: with zero content paths nothing else is
  in the set. The note (always written in this case, since there are
  deletions) starts its rationale with
  `delete.me is an empty placeholder: every change in this worktree was a
  deletion, and a shelf set cannot be empty.`
- Stores one set via the same tar builder as `ShelfAddFiles` (refactored to
  take `(name, bytes)` members) → `shelf.Store.PutFiles("", origin, tar,
  label)`, origin `{Worktree: dir, Branch: branch, State: StateStaged}`,
  label `WIP on <branch>` (`detached` when HEAD is detached).
- If anything was deleted or renamed: `NoteAdd` a shelf-level note, source
  `agent`, author `gg`, summary `Recycled from <dir> (<branch>)`, rationale:

  ```
  Deleted (not in this set):
    a.go
    b/c.txt
  Renamed (stored under the new path):
    old.go → new.go
  ```

  (only the sections that apply; the `delete.me` line first when the
  placeholder was added). The note store is a local file: no gate.
- Note failure → remove the just-stored entry and return the error.

### Atomicity

Nothing destructive happens until the entry (and its note, when needed) is
stored. Failure at 1 or 2 returns an error; the target keeps its changes (now
staged, which is the only visible difference — acceptable and documented in
the error path). A failure at 3/4 behaves as the existing discard/switch
failures do; the shelf entry stays (the work is safe).

### Wiring

- Engine: option list, `case "shelve"`, `Progress{Step: "shelving"}`,
  `AppendSummary("; shelved as %q", label)` literal, `ErrNoShelve`.
- TUI: `optionDisplayName` case `shelve`, `opAffectedSources` adds the shelf
  source for `RecycleWorktree` (a shelved answer changes the shelf list).
- CLI: `--on-dirty` accepts `shelve`; usage + help.
- Bundles ja/ko/zh/ru: `shelve`, `; shelved as %q`, `shelving`, "View note",
  the viewer's strings.
- e2e scenario: dirty target (modified + untracked + deleted + renamed) →
  `--on-dirty=shelve` → branch switched, target clean, one shelf entry
  `WIP on <old>` holding the modified/untracked/renamed-new files, one note
  listing the deletion and the rename.
- Docs: CHANGELOG, README (w row / shelf), using-gg skill (+ version bump),
  `docs/CLAUDE-details.md` (seam + decision), CLAUDE.md engine row seam list.

## Out of scope / follow-ups

- **DONE (feat/shelf-note-rows):** **Shelf-entry notes in the file tree (user, 2026-09-29).** Browsing a shelved
  set (`G` → enter) should list the entry's notes as rows of its file tree,
  the way a commit's AI reviews appear there (the `@notes/` rows of the
  commit files view), so the note is read in place — not only via `n` in
  the switcher. Built as a Notes section (path-less `shelfNote` rows, enter =
  the note popup) plus a prose element per note in the stacked view (user
  ruling: "add notes to stack view, same as review overview").
- Users adding/editing shelf-entry notes; notes on other entry kinds
  (bookmarks, saved compares).
- MCP read of shelf-entry notes.
- Web recycle UI (existing follow-up), remote-only branches (existing).
- Restoring a shelved set does not re-apply its deletions/renames (the note
  tells the user what to do by hand).
