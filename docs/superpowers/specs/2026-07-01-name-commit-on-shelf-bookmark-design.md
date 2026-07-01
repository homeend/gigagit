# Name a commit when shelving / bookmarking it

Date: 2026-07-01
Status: Approved (brainstorm) — ready for implementation plan

## Summary

When a user shelves a commit ("Shelf this commit" / `gg shelf commit`) or
bookmarks a commit ("Bookmark this commit"), let them give it a human **name**
so it is easy to distinguish later in the shelf `G` / bookmark `g` switchers.
The name is captured **at creation time only** (no rename-later).

## Current state (what already exists)

- **Bookmarks** already carry a `Label` field; `bookmarkDisplay` already renders
  `… — <label>` for commit bookmarks; and `gg bookmark add --rev <sha> --label …`
  already names a path-less commit bookmark. The gap is only the **TUI**: the
  "Bookmark this commit" row fires immediately with `Label = commit subject`,
  never prompting.
- **Shelf entries have no `Label`**. The shelf switcher shows `Origin.Display()`
  (e.g. `commit / a1b2c3d`). `gg shelf commit <sha>` has no name option. So the
  shelf side needs a new label field end-to-end plus the TUI prompt.

## Interaction (TUI)

Picking **"Shelf this commit"** or **"Bookmark this commit"** — on both the
Commits panel and the Reflog panel — opens a single-line **name popup** instead
of firing immediately:

- The field is **pre-filled with the commit subject**, editable. **Enter**
  accepts (one keystroke for the default) or type to replace.
- **`ctrl+s` inserts the commit's short SHA (`sha[:7]`) at the cursor position**,
  so a user can build e.g. `a1b2c3d — my fix`. Advertised in the hint line as
  `[ctrl+s] insert sha`.
- **Enter** creates the shelf entry / bookmark with the entered name. **Esc**
  cancels the whole action (nothing is created).
- **Empty field on Enter** → fallback: a bookmark uses the commit **subject**
  (today's behavior); a shelf entry uses no label, so its switcher row falls back
  to `Origin.Display()` (`commit / <sha>`).

A single shared popup type drives both actions and both panels:
`commitNamePopup{ name textfield; sha string; subject string; forShelf bool }`
implementing the `layer` interface (`update`/`render`). Its `run`-equivalent on
Enter dispatches the correct create command (shelf vs bookmark) with the label.

## Architecture

### model (`internal/model`)
- Add `ShelfEntry.Label string` (TOML-persisted; empty = no label). Mirrors
  `Bookmark.Label`.

### shelf store (`internal/shelf`)
- `PutCommit(bucket string, addr model.FileAddress, tar []byte, label string)
  (model.ShelfEntry, error)` — records `label` on the entry. The derived entry
  **ID is unchanged** (`commit-<shortsha>-<blobsha8>`); the label is display-only.

### domain (`internal/domain`)
- `ShelfAddCommit(ctx context.Context, sha, label string) (model.ShelfEntry, error)`
  — threads `label` into `PutCommit`. (All call sites updated.)

### tui (`internal/tui`)
- New `commit_name_popup.go`: the shared `commitNamePopup` + its dispatch.
- `commitShelfRow`/`reflogShelfRow`/`commitBookmarkRow`/`reflogBookmarkRow` change
  their `run` handlers to `pushLayer(&commitNamePopup{…})` instead of firing the
  create command directly. The popup, on Enter, calls the existing
  `shelfAddCommitCmd`(now taking a label) / `bookmarkAddCmd` with the label.
- `commitBookmark(c model.Commit, label string) model.Bookmark` takes the entered
  label (falls back to `c.Subject` when empty).
- Shelf switcher (`shelf_popup.go`): the per-row text becomes
  `shelfEntryDisplay(e)` = `e.Origin.Display()` plus ` — <Label>` when
  `e.Label != ""` (mirrors `bookmarkDisplay`).
- `textfield` (`textfield.go`): add `InsertString(s string)` — insert `s` at the
  cursor, advancing the cursor past it (pure; unit-tested). Used by the popup's
  `ctrl+s` handler with `sha[:7]`.
- Advertise `[ctrl+s] insert sha` in the popup hint, and note the naming prompt
  in `help.go` where the "Shelf/Bookmark this commit" actions are described.

### cli (`internal/cli`)
- `gg shelf commit [--name <name>] <sha>` — flag before positional (Go flag
  parsing); `--name` empty/absent → no label. Bookmark CLI already supports
  `--label`, so no change there.

## Error handling

- Esc / empty-after-cancel → no entry/bookmark created, popup closed, no status
  error.
- The create still goes through the existing async command + `shelfAddedMsg` /
  `bookmarkAddedMsg` handlers; their existing error paths are unchanged.
- `ctrl+s` when the sha is short (< 7 chars, e.g. a synthetic rev) inserts the
  full sha string as-is (guarded slice).

## Testing

- **textfield**: `InsertString` inserts at the cursor mid-string, at start, at
  end; cursor advances correctly (pure unit test).
- **commitNamePopup**: Enter with a typed name dispatches a non-nil create cmd;
  Esc closes with no cmd; `ctrl+s` inserts `sha[:7]` into the field value; the
  `forShelf` flag routes to shelf vs bookmark.
- **domain/shelf**: `ShelfAddCommit(sha, "my name")` persists `Label`; a reloaded
  entry keeps it (TOML round-trip); `PutCommit` with `label=""` leaves it empty.
- **tui display**: `shelfEntryDisplay` shows `… — my name` when labeled, plain
  `Origin.Display()` when not.
- **cli**: `gg shelf commit --name "my name" <sha>` creates a labeled entry
  (assert via `shelf list` / the entry's Label).

## Docs to update on completion

`CHANGELOG.md` (always); `README.md` (the naming prompt + `gg shelf commit
--name`); `CLAUDE.md` (shelf row: `ShelfEntry.Label`; note the naming popup);
`internal/agentskill/using-gg.md` + `agentskill.Version` bump (CLI `--name`).

## Out of scope (YAGNI)

- Renaming an already-shelved/bookmarked commit (creation-time only, per request).
- Naming file shelves/bookmarks (they already have a distinguishing path).
- Changing the derived entry ID to include the label.
