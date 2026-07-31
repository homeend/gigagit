# Web UI: staged commit-detail layout (GitKraken-style drill-in)

Date: 2026-07-31
Branch: `feat/web-staged-detail` (off `web-dev`)

## Problem

Clicking a commit today jumps straight to a two-pane detail screen — file
list left, diff right, first file auto-opened — and the commit list
vanishes. The user wants GitKraken's staged flow: first browse commits
with a file list alongside, and only see a diff after deliberately
picking a file.

## The three stages

Today `state.layout` has two values (`list`, `detail`); it gains a third,
and `detail` is renamed to what it now means:

1. **`list`** (unchanged) — sidebar | commits, full width.
2. **`files`** (new) — entered by clicking a commit (or the working-tree
   row, a stash, a tag, or opening a branch compare): the commits pane
   stays on the left, shrunk to the flexible column, and the file list
   appears as a fixed-width column on the **right**. No diff pane, nothing
   auto-opens. Clicking another commit in the shrunk list swaps the file
   list in place.
3. **`diff`** (replaces `detail`) — entered by clicking a file (or Enter
   on the file cursor): the diff takes the flexible left column, the file
   list stays right. Clicking other files swaps the diff; the diff-header
   file/change arrows keep working.

**Escape steps back one stage**: `diff` → `files` → `list`. The `← back`
button and the footer chip share the same stepping (`drillOut`). The
sidebar STAYS through the files stage (revised from the first cut after
live use — GitKraken keeps its left panel too) and hides only on the
diff screen; `exitStatusToList` (working tree went clean) still drops
straight to `list`.

All five detail entry points move to the staged flow — `openCommit`,
`openCommitByHash` (tags), `openStashDetail`, `openWorkingTree`,
`openCompare` — by setting layout `files` and dropping their
`openFile(0)` auto-open. `openFile` itself switches to layout `diff` in
its synchronous prefix (before the fetch), so an esc during a slow diff
load cannot be undone by the fetch completing.

## Mechanics

**Grid.** `#panes` keeps the exactly-three-visible-children rule in
`list` and `diff`; `files` is the one five-children mode (its `nosb`
variant drops back to three):

| mode  | class    | children (left → right)                          | columns |
|-------|----------|--------------------------------------------------|---------|
| list  | `solo`   | branches, rs-sidebar, commits                    | `var(--sb-w) var(--rs-w) 1fr` (unchanged) |
| files | `files`  | branches, rs-sidebar, commits, rs-detail, files  | `var(--sb-w) var(--rs-w) 1fr var(--rs-w) var(--files-w)` |
| diff  | `detail` | diff-pane, rs-detail, files-pane                 | `1fr var(--rs-w) var(--files-w)` |

With both fixed columns on screen at once in the files stage, each
handle's clamp also reserves the OTHER handle's width
(`clampPaneWidth(cfg, w)`), or the flexible commits column between them
could be squeezed to nothing.

The file list moves to the right via CSS `order` on the grid children —
no DOM moves, so every existing `$("files-list")` handler is untouched.
`.detail`'s column template flips from `files | diff` to `diff | files`.

**Resizer.** `rs-detail` used to resize the FIRST column; the file list
is now the THIRD, so its `RESIZERS` entry gains `side: "right"` and the
drag computes `panesRight - pointerX`. Same clamp (`RS_MIN`/`RS_KEEP` —
the keep-back now protects the left pane), same persistence key; the
stored magnitude means the same thing (the file list's width).

**Keyboard.** Unchanged bindings, new routing: in `files` stage j/k move
the file cursor (the pane is focused on entry, as today), Enter opens the
diff (stage 3). Esc in the global router calls the reworked `drillOut`.
Status-mode `s`/`u` staging works in both stages.

**Diff-pane hygiene.** Entering `files` clears `#diff-title`/`#diff-body`
so a later stage-3 entry never flashes the previous drill's diff.

## Not changing

- Server: zero changes — this is `index.html`/`style.css`/`app.js` only.
- `state.pane` focus model, files-list rendering, hunk staging, compare
  bar, diff-nav — all untouched except where the layout class is read.
- The sidebar `b` toggle works in `list` AND `files` (both show the
  sidebar); only the diff screen refuses it (nothing to toggle).

## Verification

Headless CDP with geometry assertions (`getBoundingClientRect` +
`elementFromPoint`):

- commit click → commits pane visible AND narrower than before, files
  pane right of it, diff pane `display: none`, no diff fetched.
- file click → diff pane visible with `diff.left < files.left`, commits
  pane hidden.
- esc once → back to stage 2 (diff hidden, commits back); esc again →
  full-width list.
- same staged flow for the working-tree row and a branch compare.
- rs-detail drag in stage 2 widens/narrows the RIGHT column.
