# View file content in a right-pane preview — Design

**Goal:** In full-tree mode (`a`) of the commit files view, add a `.`-menu
**View file** action that shows the selected file's content *at that commit* (no
diff, just the version) in the right column — replacing the Commits panel — while
the file tree stays on the left.

**Status:** Approved (user chose the right-pane preview), v1.

## State (Model)

- `filesPreview *contentPopup` — the read-only content viewer (nil = none). Reuses
  `contentPopup` for scroll/`z` display-mode; one line per file line.
- `filesPreviewTag string` — `<path>@<hash>`, gates stale async `ShowFile` results.

## Action

- `viewFileRow() (actionRow, bool)` — added in `availableActions`' content-window
  branch, present only when `m.filesView != nil && m.filesAllFiles &&
  m.filesTreeFocused` and the selected tree row has a non-empty path. Its `run`
  handler sets a `(loading…)` preview + tag, focuses the right side
  (`filesTreeFocused = false`, so the cursor lands in the preview to scroll), and
  dispatches `loadFileContentCmd`.

## Load (off-thread)

- `loadFileContentCmd(hash, path, tag) tea.Cmd` → `svc.ShowFile(hash, path)` →
  split bytes into `[]contentLine` off the UI thread → `fileContentMsg{tag,
  lines, err}`. A file over `domain.MaxDiffBytes` is shown as a single
  "(file too large to preview)" line (no split).
- `case fileContentMsg`: apply only when `m.filesPreview != nil && msg.tag ==
  m.filesPreviewTag` (else stale/closed).

## Render

- In `renderInterface`'s right-column branch, `if m.filesPreview != nil` →
  `renderFilePreview(rightW, h)` instead of the Commits panel. Window-then-build
  (a file can be large) with a focus-following border (focused when the preview
  side owns the keyboard). Title = the path; the tree stays on the left.

## Key routing (the trap — centralized)

All four keyboard movement sites (`up`/`k`, `down`/`j`, `pgup`, `pgdown`) **and**
the mouse wheel funnel through `moveListUnderFilesView`. So preview scrolling is
handled in exactly one place:

```
func (m Model) moveListUnderFilesView(delta int) (tea.Model, tea.Cmd) {
    if m.filesPreview != nil { m.filesPreview.move(delta); return m, nil }
    ... existing stash / commit routing ...
}
```

Other key adjustments while a preview is open:
- `esc` closes the preview first (before clearing search / closing the view).
- `z` cycles the **preview's** display mode when the preview side is focused.
- The graph keys (`<`/`>`/`=`/`shift+←/→`) and `/` (commit filter) are gated with
  `m.filesPreview == nil` — the graph/commit list isn't shown under a preview.
- `enter` on the right side stays a no-op (the preview is read-only); `enter` on
  the tree side still diffs.

## Clearing the preview

`esc` (focused); closing the files view (`esc`/`l` on the tree side); the `a`
toggle (it is a full-tree concept); `openCompareFiles`; and re-opening the files
view (`l`). Because `moveListUnderFilesView` scrolls the preview rather than the
commit list while it is open, `filesHash` cannot change under a live preview, so
there is no stale-commit case from navigation.

## Testing

- `viewFileRow` present in full-tree + tree-side on a file; absent in
  changed-files mode, off the tree side, on a heading/empty row.
- Running it sets `filesPreview` + a cmd; `fileContentMsg` (matching tag)
  populates lines; a stale tag is dropped.
- Render: the right column shows the file's content (title + a content line)
  instead of "Commits" when the preview is active.
- Routing: with the preview open and the right side focused, `j` scrolls the
  preview (its `sel` advances), `esc` closes it (preview nil, files view still
  open); the graph key `>` is a no-op while a preview is open.
- Closing the view / toggling `a` clears the preview.

## Out of scope (v1)

Search within the preview (`/`); a live preview that follows the tree cursor
(that was the rejected option); syntax highlighting.
