# gg web — `/` search in overviews (spec)

Date: 2026-10-25 · Branch: feat/web-overview-search · TUI is the spec.

## Gap

The TUI answers `/ @ ] [` (previewSearchKey) on three overview surfaces the
page did not search:

1. an overview opened on its own (TUI `file_viewer.go`, under 60 columns; web
   `overviewKey`, under 900px);
2. the split's left column, the overview (TUI `split_overview.go:49`; web
   `splitOverviewKey`);
3. the split's right column in stacked mode (TUI `split_stack_pane.go:534`,
   re-find on every rebuild at `:181`; web `stackKey` said "this acts on a
   single file — S opens it").

## TUI captures (tui-capture.sh, agent overview fixture with 6 × "needle")

- Standalone (58 cols): `/needle` → `/needle█ 1/6` in the title row, `]` →
  `2/6`, esc clears the bar, the next esc closes.
- Split left (140 cols, first link selected — it sits after the first hit):
  `/needle` → `2/6` in the LEFT title row; `]` → `3/6`; esc clears.
- Stack (two files, main.go + markdown.js): `/mdHTML` → `1/3`, the title
  goes to `file 2/2`, the cursor lands on the hit's line; `]` → `2/3` →
  `3/3`; `[` → `2/3`.

## Side effects on screen

- The bar shows in the column that has the keys: the left column gets its
  own bar under its title; the right file / stack uses the viewer's bar.
- The count `i/n`; every hit tinted, the current one outlined.
- Overview: the link selection (`.asel`) never moves with a hit; the search
  starts from the current hit while on screen, else the selected link
  (from its offset in its block), else the first block on screen.
- Stack: the cursor (`.vcur`) lands on each hit's line or header, so the
  title's `file k/n` follows; a waiting link step is dropped.
- esc while typing puts the view (and the stack cursor) back; esc on a kept
  query clears it, only the next esc closes / steps aside.
- Footers name `/ find` and `] [ next / prev` on every overview surface.

## Design

- `docsearch.js`: `docSearchHost` — rows are the painted markdown's text
  blocks (`p, .md-h, th, td, pre, .md-cap`), hits laid over text nodes
  (`paintRow`, moved here from helpsearch.js) and unwrapped again, so the
  anchors' elements and classes are never rebuilt. Standalone uses the
  viewer's bar (`viewerDoc`), the left column its own (`ovDocSearch`).
- Stack: `splitstack.js stackStream` (the TUI's `fileStream.build`: header,
  then a shown file's lines) searched whole per key with folds cached per
  slot; painted near the screen by `paintwin.js windowPainter`. A file
  read / fold / unfold re-finds, keeping the current hit's (file, line).
  While a search is active the stack reads every unfolded file, as the TUI's
  stack does.

## Differences (recorded in docs/web-tui-parity.md)

- Rows are blocks, not wrapped rows: a query may match across a wrap.
- A stack header is searched as drawn (path cut in the middle).
