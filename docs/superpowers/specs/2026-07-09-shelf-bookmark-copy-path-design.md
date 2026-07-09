# Copy file path/name from the shelf & bookmark switchers — Design

**Date:** 2026-07-09
**Status:** Approved
**Surface:** TUI only (shelf switcher `G`, bookmark switcher `g`)

## Problem

A shelf entry and a bookmark both carry the file's repo-relative path as
structured metadata (`model.ShelfEntry.Origin.Path`, `model.Bookmark.Path`) —
it is shown in the switcher rows via `FileAddress.Display()` — but there is no
way to get that path (or just the file name) onto the system clipboard from
either switcher. The Files-panel `.` menu already has "Copy file path" /
"Copy file name" rows (`fileCopyPathName`, `internal/tui/action_menu.go`);
the switchers lack the equivalent.

**No new metadata is stored.** The request's "add metadata with the file
name" is already satisfied by the existing structured fields; this feature
only adds copy affordances that read them.

## Behavior

In both quick-switchers, pressing **`y`** on a highlighted **single-file
entry** opens a small chooser modal while the switcher stays on the layer
stack (the shelf remove-prompt pattern, `shelfPopupRemovePrompt`,
`internal/tui/shelf_popup.go`; visually the modal replaces the switcher box
until it resolves — Cancel reveals the switcher unchanged):

```
Copy — <repo-relative path>
  Copy file path
  Copy file name
  Cancel
```

- **Copy file path** → clipboard gets the repo-relative path; status line
  shows `Copied path: <p>` (exact string reused from `fileCopyPathName`).
- **Copy file name** → clipboard gets `path.Base(p)`; status line shows
  `Copied file name: <base>`.
- **Cancel** → modal closes, switcher revealed unchanged.
- `Cancel` is the **last** option so the modal's esc mapping (`abortOption`:
  option named "abort" if present, else the last option) resolves esc to a
  genuine cancel.

The clipboard write goes through the existing `copyToClipboardCmd`
(`internal/tui/clipboard_cmd.go`) — native OS command with OSC 52 fallback;
a failure surfaces as the existing `copy failed: …` status.

### What is copied

- Shelf: `entry.Origin.Path` (repo-relative, as captured at shelve time).
- Bookmark: `bookmark.Path` (repo-relative).
- File-name variant: `path.Base(...)` of the same value (import `path`, not
  `path/filepath` — these are git-style slash paths, matching
  `fileCopyPathName`).

### Gating

- **Commit entries:** `y` on a shelved commit (`ShelfEntry.IsCommit()`) or a
  path-less commit bookmark (`Bookmark.IsCommit()`) does NOT open the modal;
  it sets the existing notices instead:
  - shelf: via `commitShelfNotice` — `"not available for a shelved commit — [t] copies it to a temp dir"`
  - bookmark: via `commitBookmarkNotice` — `"not available for a commit bookmark"`
- **Compare-picker mode** (`p.compareRef != nil` in either popup): `y` is
  inert (`return m, nil`), consistent with `e`/`p`/`x`/`m`/`c` in that mode.
- **Filter sub-mode** (`p.filtering`): `y` is filter text, as today — the new
  case lives in the non-filtering rune switch only.
- **Empty selection** (`p.selected()` not ok): no-op.
- **Busy** (`m.running`): already handled — both popups return early before
  the key switch.

## Implementation shape

### Shared modal builder (new, small)

One helper used by both popups, placed in `internal/tui/clipboard_cmd.go`
beside `copyToClipboardCmd`:

```go
// copyFilePrompt opens a path/name copy chooser for a repo-relative file
// path. The modal renders above the calling popup (which stays on the
// layer stack); Cancel/esc reveals it unchanged.
func (m Model) copyFilePrompt(p string) (Model, tea.Cmd)
```

It sets `m.modal = &decisionState{...}` with:

- `ID: "copy-file"`
- `Prompt: "Copy — " + p`
- `Options: []string{"Copy file path", "Copy file name", "Cancel"}`
- `onResolve`: maps the option through `copyFileChoice` (below); a hit →
  `m.copyToClipboardCmd(okMsg, text)`, a miss (Cancel/unknown) →
  `return m, nil`.

The option→payload mapping is a pure helper beside it, so the exact strings
are unit-testable without touching the real clipboard:

```go
// copyFileChoice maps a copy-chooser option to its status line and clipboard
// text. ok is false for Cancel or an unknown option.
func copyFileChoice(option, p string) (okMsg, text string, ok bool)
```

- `"Copy file path"` → `("Copied path: "+p, p, true)`
- `"Copy file name"` → `("Copied file name: "+path.Base(p), path.Base(p), true)`
- anything else → `("", "", false)`

### Call sites

- `internal/tui/shelf_popup.go` — `shelfPopup.update`, rune switch: new
  `case "y":` after `case "t":`. Order: compare-mode guard →
  `commitShelfNotice` guard → `p.selected()` → `m.copyFilePrompt(e.Origin.Path)`.
- `internal/tui/bookmark_popup.go` — `bookmarkPopup.update`, rune switch:
  same structure with `commitBookmarkNotice` and `m.copyFilePrompt(b.Path)`.

### Discoverability (advertise in help AND footer)

- Hint lines: add `"[y] copy"` to both popups' hint slices (shelf:
  `renderShelfPopupBox`; bookmark: the `hint := []string{...}` at
  `bookmark_popup.go:149`), placed after `[t] temp dir`.
- Cheat sheets: add a `y — copy file path / name to the clipboard` line to
  both `bookmarkSwitcherHelp` and `shelfSwitcherHelp`
  (`internal/tui/popup_help.go`), in the non-compare key list.
- `internal/tui/help.go`: extend the `g` and `G` switcher lines to mention
  `y` copies the path/name.

## Not in scope (deliberate)

- Copying the sha of a shelved commit / commit bookmark (`y` shows the
  notice instead) — the Commits panel `.` menu already covers sha copying.
- Absolute-path variant (joining `Origin.Worktree`) — repo-relative matches
  the Files-panel convention.
- CLI changes (`gg shelf list` / `gg bookmark list` already print the path
  in the origin display) and any store/model change.

## Error handling

- Clipboard failure: existing `clipboardCopiedMsg{err}` path → status
  `copy failed: …`. Nothing new.
- No path (defensive: a file entry with an empty `Origin.Path`/`Path`
  shouldn't exist; commit entries are guarded before the modal): the
  guards make this unreachable, no special handling.

## Testing

Unit tests in the existing popup-test style (`shelf_popup_test.go`,
`bookmark_test.go` conventions — build the popup, drive `update` with key
msgs, assert on `m.modal` / returned cmds / `statusMsg`):

1. `y` on a file shelf entry opens the modal: ID `copy-file`, prompt names
   the path, options exactly `["Copy file path", "Copy file name", "Cancel"]`.
2. `copyFileChoice` direct unit test: both copy options yield the exact
   `(okMsg, text)` pairs above; `"Cancel"` and an unknown option yield
   `ok == false`. The modal test asserts `onResolve` returns a non-nil cmd
   for the two copy options and a nil cmd for Cancel — the cmd is never run
   (running it would write the real clipboard; `clipboardCopiedMsg` handling
   is already covered by `clipboard_cmd_test.go`).
3. `y` on a shelved-commit entry: no modal, `statusMsg` is the shelf notice.
4. `y` on a commit bookmark: no modal, `statusMsg` is the bookmark notice.
5. `y` in compare mode (either popup): no modal, no cmd.
6. `y` while filtering appends to the filter (regression guard).
7. Hint/cheat-sheet text contains `[y] copy` / the `y` help line (render or
   direct-call asserts).

Same suite for the bookmark popup where behavior differs (notice text,
`b.Path` source).
