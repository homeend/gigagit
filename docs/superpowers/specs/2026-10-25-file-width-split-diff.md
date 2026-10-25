# [ui] file_width for the overview split's file and the diff sides

Asked 2026-10-25, after file_width shipped for a file shown alone (specs
2026-10-25-web-file-width): "apply limit to side-by-side file view on the
right (it should be max size of column, if space is available). plus given
value should be limit for file diff side-by-side max width for single file
(left and right) so on wide monitors they are not all over the screen".

## What changes

1. **The overview split's right column** (TUI and web): the file — or the
   stacked files — shown beside an overview lays its text in a centred
   column of gutter + N characters inside its half; a column narrower than
   that is used whole. The overview (left column) is unchanged.
2. **Diffs** (TUI and web): each side of a side-by-side diff shows at most
   N characters of text; the two sides (with their line numbers) sit
   together, centred. The web's unified layout (a narrow pane) and the
   one-sided table of an added / deleted file take the same cap on their one
   text column. This covers every diff drawn as a diff table: the diff pane,
   the stacked commit view, the history overlay, a PR's diffs; the TUI's
   diff view and its stack.

N = `[ui] file_width` (default 200, -1 = unlimited): no new setting.

Not changed: images (they keep the whole width), the overview HTML export
(no script, so its stylesheet's fallback = unlimited).

## Layout

- TUI diff: one accessor `paneW()` = min((width - 1) / 2, gutter + 1 + N)
  replaces every `(w - 1) / 2` (render, relayout, textWidth, notes, the
  character selection, the mouse, images keep the full width). The body
  rows are centred in the full width; the title line and the keys keep the
  full width (a narrow column never cuts the way out). A click maps through
  the margin. N lives on the view (relayout runs without a Model) and is
  refreshed when the setting changes.
- TUI split: the right pane passes `single` (previewColumn already exempts
  overviews); every key / click geometry of a pane uses the same column.
- Web diff: `max-width` per `data-cols` (2: no + side; 3: 2·no + side;
  4: 2·(no + side)) and `margin: 0 auto`, from a second measured variable:
  table.diff text is 13px (the page font), not the code bodies' 12px.
- Web split: `#viewer-box.split #viewer-body` gets percentage side padding
  (`max(0px, (100% - text - gutter) / 2)`), so the right column's rows sit
  centred at most N wide.

## What the user will see at file_width = 120 on a 1920px screen

- Diff pane: about 1500px with the sidebars; two sides at 120 characters
  need about 2 × (46 + 12 + 120 × 7.8) ≈ 2000px — wider than the pane, so
  nothing changes. The cap binds on wider screens, or at about 80 on this
  one.
- Split right column: half of 94vw ≈ 900px ≈ 115 characters at 12px —
  120 just misses; about 100 shows it.

## Side effects on screen

- A stacked diff's file headers (`.stk-head`) span the side-by-side table's
  width, centred over it (decided by screenshot: a pane-wide header put the
  `+13 −0` counts far from the diff).
- Scroll mode's pan bars span the table they drive (`.hbars[data-cols]`).
- TUI: the width reaches every open diff through Model.Update
  (syncDiffWidths: the diff layers and the history view's right pane), so
  none of the ~28 places that build a diffView carries it; a change re-wraps
  with the viewport anchored to its top line, as a resize does.
- The TUI's fold separators and stack header rows take the capped width, so
  they line up with the sides.

## Tests

- TUI: render at 300 columns with N = 40 → each side ≤ 40 text columns,
  centred; title and keys full width; textWidth / charsel / mouse use the
  capped pane; -1 unchanged; split pane activePreview innerW = capped.
- Web: pins on the CSS rules and the measured variable; probe (Chromium +
  Firefox, 2560px) measures each side's text columns, the stacked view, the
  history overlay, the split's right column.
