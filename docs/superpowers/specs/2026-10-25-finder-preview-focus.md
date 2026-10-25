# F window: the preview takes the keys (TUI + gg web)

User report (2026-10-25): in gg web's F window, enter on a file opens the
actions menu and the keys never reach the previewed file — no paging, no
search. Rulings from the user: enter hands the keys to the preview on BOTH
frontends (the menu stays on `.`), and the web ports the TUI preview's line
cursor and marks too.

## TUI (the spec)

- `enter` on a file row = `→` / `tab`: the preview takes the keys. A heading
  row stays inert. `.` alone opens the actions. Footer:
  `[.] actions … [enter/→] preview`; the `?` help says the same.

## gg web — the F preview with the keys

Entering: `enter` (file row), `→`, `tab`, or a click anywhere in the preview
(the click also puts the line cursor on the clicked line). Leaving: `←`,
`tab`, `shift+tab`, `esc` (after marks / a kept query are cleared), or a
click on the list. The side holding the keys is shown: its title is
accented, the other side's cursor row is dimmed, and the bottom bar shows that
side's keys.

Keys in the preview (TUI `updateWorktreePreviewKey` + `previewSelectKey` +
`previewSearchKey` + `previewJumpKey`; the browser reserves ctrl+t / ctrl+w,
so the web's own letters stand in, as in the viewer and history overlays):

| key | TUI | web |
|---|---|---|
| ↑ ↓ j k | scroll a line | same |
| pgup pgdn | scroll a page | same |
| home end | top / bottom | same |
| alt+↑ alt+↓ | line cursor; a cursor scrolled out re-enters at the window's edge | same |
| space | mark: start, then freeze the far end, then start again | same |
| enter (marks) | copy the marked lines | same |
| L (marks) | copy the link to the marked lines | same |
| esc | unmark → clear the query → back to the list | same |
| / @ ] [ | in-view search | same; hits painted near the screen only (the blame rule) |
| : | go to line | same |
| ctrl+t | preview fills the screen | `m` |
| ctrl+w | long lines: wrap / scroll / cut | `w` |
| ctrl+] | open in the background | same |

A new file under the list cursor resets the cursor, marks and search; the
same file re-read from disk keeps them (clamped).
