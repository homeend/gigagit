# Hunk picker display modes (cutoff / wrap / scroll) — design

**Date:** 2026-06-16
**Status:** approved (brainstorm), pending spec review

## Problem

The shared `hunkPicker` surface (conflict resolver + hunk staging) truncates
every candidate line to its half-width column, so long lines are cut off and you
cannot see what you are choosing from. It also renders **every** row with no
vertical scrolling, so large hunks run off the bottom of the screen.

## Goal

Give the picker the same display-mode behavior as every other window in the app:
**`z`** cycles **cutoff → wrap → scroll**, **shift+←/→** pans horizontally in
scroll mode — defaulting to **scroll** (so long lines are immediately readable by
panning). Add the shared **vertical scroll** the surface lacks. Keep the
two-column side-by-side layout; in wrap mode keep left/right pairs aligned.

Plain `←/→` continues to switch the focused side (current↔incoming /
index↔working) — which is precisely why panning is bound to **shift**+arrows.
Both consumers of the shared picker (conflict resolve and staging) get this; both
default to scroll.

## Architecture

The insight: the window abstraction (`renderWindow` + its leaf transforms)
already drives sub-viewport windows — the three left panels are half-width
windows that cutoff/wrap/scroll today. The picker is just **two such windows
side by side**, with the surface (parent) owning key dispatch, the 2D cursor,
and — the one coordination point — a **single shared vertical scroll** (the two
columns must show the same hunks, so they cannot scroll independently) and a
**single shared mode + hscroll** (`z`/shift act on "the picker", both columns
together).

The missing piece is the **content model**: today a window row is a plain
`winRow{text, style}`, but the picker needs rows whose cells carry a **fixed
gutter** (the cursor `>` and the `[x]`/`[ ]` tick) that must never scroll away,
plus a **body** that the mode transforms. That gutter+body cell is the reusable
generalization.

### Component 1 — `internal/tui/twocol.go` (new, reusable)

A pure layout helper composed from the existing leaf transforms
(`truncate` / `wrapWidth` / `hslice` / `windowStart` in `window.go`) — it does
**not** modify `renderWindow` or its five existing consumers (blame, conflict
popup, action menu, panels).

```go
// winCell is one cell of a two-column window: a fixed gutter (shown verbatim,
// never transformed — the cursor marker + checkbox live here) and a body that
// the display mode transforms. style is applied to the cell's display segments
// after slicing (ANSI-safe).
type winCell struct {
	gutter string
	body   string
	style  lipgloss.Style // zero value = unstyled
}

// colRow is one logical row: a full-width row (full != nil, spanning both
// columns) OR a paired left/right row (left != nil && right != nil).
type colRow struct {
	full        *winCell
	left, right *winCell
}

// twoColOpts: w/h are the surface body size; sep is the column separator
// (" ║ "); mode + hscroll are the shared display state; anchor is the logical
// colRow index kept visible (the cursor's row).
type twoColOpts struct {
	w, h    int
	sep     string
	mode    dispMode
	hscroll int
	anchor  int
}

// renderTwoCol lays rows out under o and returns exactly o.h display lines, each
// padded to o.w columns.
func renderTwoCol(rows []colRow, o twoColOpts) []string
```

Behavior:
- Column width `colW = (w - len(sep)) / 2` (matches today's picker math).
- For each `colRow`, compute display segments under `o.mode` applied to each
  cell's **body** (gutter prepended verbatim, reducing the body's usable width):
  - **cutoff:** one segment, `truncate(body, bodyW)`.
  - **scroll:** one segment, `hslice(body, hscroll, bodyW)`.
  - **wrap:** `wrapWidth(body, bodyW, …)` → N segments; the gutter shows only on
    the **first** segment, continuation segments are blank-padded to the gutter
    width (so wrapped text stays under its own column, like the diff view).
- **Full-width** rows transform across the whole `w` (one cell, no pairing).
- **Paired** rows: take `n = max(leftSegs, rightSegs)`; for each k in `0..n`
  emit `pad(leftGutter+leftSeg[k], colW) + sep + pad(rightGutter+rightSeg[k],
  colW)`, blank-padding the side that ran out — **this is the aligned-pair
  guarantee** (decision (a)).
- Apply each cell's `style` to its own segments after slicing.
- Flatten to display lines, tracking which display line each `colRow` starts on;
  the anchor row's first display line feeds `windowStart(total, h, anchorLine)`
  to pick the visible slice (shared vertical window).

### Component 2 — `hunkPicker` (in `internal/tui/conflict_picker.go`)

- New fields: `mode dispMode` (default `modeScroll`), `hscroll int`.
- Both constructors (`newConflictPicker`, `newStagePicker`) set `mode:
  modeScroll`.
- `update` gains:
  - `"z"`: cycle in the order **scroll → wrap → cutoff → scroll** and reset
    `e.hscroll = 0`. Note this is the *reverse* of the app-wide
    `dispMode.next()` (cutoff→wrap→scroll); given the enum layout
    (cutoff=0, wrap=1, scroll=2) it is a decrement, which yields the requested
    order starting from the scroll default. A tiny picker-local
    `cyclePickerMode(dispMode) dispMode` keeps this intent explicit rather than
    reusing `next()`.
  - `"shift+left"`: if `e.mode == modeScroll`, `e.hscroll = max(0, e.hscroll -
    hscrollStepConst)`.
  - `"shift+right"`: if `e.mode == modeScroll`, `e.hscroll += hscrollStepConst`.
  - (a small constant step, e.g. 8 — the picker is a surface and does not have
    access to `m.hscrollStep()`'s config the way panels do; a fixed step is
    fine, matching the diff view's default.)
- `render` builds `[]colRow`:
  - hunk header → `full` cell (styled focus/dim).
  - literal passthrough lines → `full` cells (dim).
  - candidate rows → paired `left`/`right` cells; gutter = `"> "`/`"  "`
    (cursor) plus `"[x] "`/`"[ ] "` (line-by-line tick) ; body = the line text;
    the focused cell's `style = selectedRow`.
  - result-preview lines (line-by-line) → `full` cells (dim).
  - The trailing key-hint line stays outside the scrolled body (rendered after,
    like today) and reflects the keys; the body height excludes it.
- The cursor's candidate row index is the `anchor`.
- Footer hint gains `[z] mode  [shift+←/→] scroll` alongside the existing keys.

The `cell` free function and the hard-coded `truncate`-based row building in the
current `render` are replaced by the `colRow` construction + `renderTwoCol`.

## Keys (picker, after this change)

| Key | Action |
|-----|--------|
| `←`/`→` | switch focused side (unchanged) |
| `shift+←`/`shift+→` | pan horizontally (scroll mode only) |
| `z` | cycle display mode (cutoff / wrap / scroll) |
| `↑/↓ j/k`, `n/p`, `space`, `c/i`, `C/I`, `enter`, `esc` | unchanged |

`z` and `shift+←/→` already have help rows (added by the window framework); the
picker's inline footer hint is updated. No `footerLine` registry change (the
picker is a stack surface that renders its own footer).

## Edge cases

- **Narrow width:** `colW` floors at a small minimum (as today); `bodyW` floors
  at ≥1 so transforms never divide by zero.
- **Empty side:** a paired row with an empty cell renders blank on that side
  (today's behavior); the gutter still shows the `[ ]`/cursor.
- **Cursor off-screen after navigation:** the shared vertical window re-anchors
  to the cursor row on every render, so `j`/`k`/`n`/`p` always keep it visible.
- **Mode switch resets `hscroll`** so you never land scrolled-past-content in a
  fresh mode.

## Testing

- **`twocol_test.go`** (pure): cutoff truncates; scroll reveals via `hscroll`;
  wrap emits multiple aligned lines with the **gutter only on the first**;
  paired wrap pads the shorter side so columns stay registered; the vertical
  window keeps the anchor row visible when rows exceed `h`.
- **picker tests:** default mode is `modeScroll`; `z` cycles
  scroll→wrap→cutoff→scroll (the requested order) and zeroes `hscroll`;
  `shift+right`/`shift+left` move `hscroll` only in scroll mode; a tall hunk list
  renders exactly the body height with the cursor visible. Existing
  conflict/staging surface tests stay green (the constructors/keys they exercise
  are unchanged besides the new default mode).

## Out of scope

- Refactoring the diff view onto `twocol` (a clean follow-up — it has its own
  `cellSeg`/`wrapSide` machinery today).
- `pgup`/`pgdn` paging inside the picker.
- Per-column independent mode/hscroll (the surface shares one of each).
