# Diff view word-wrap toggle (Spec B) — design

> Sibling of Spec A (diff cache + intraline emphasis, merged `0ad955e`). This
> is the deferred "word-wrap instead of horizontal scroll" follow-up.

**Goal:** A `w` toggle in the full-screen diff view that **word-wraps** long
lines across multiple display rows instead of truncating them with `…`,
keeping the two side-by-side panes row-aligned.

**Architecture:** A third, width-keyed display stage in the diff view
(`full→lines→disp`). Wrapping is a pure TUI render concern built on the
`(disp []rune, emph []bool)` representation the emphasis renderer already
produces; `textdiff`, `domain`, and `cache` are untouched. The viewport
offset becomes display-row-based; navigation and block jumps retarget onto the
display stream with no change in logic.

**Tech stack:** Go 1.26, Bubble Tea, lipgloss (width measurement). Pure TUI.

---

## Motivation

The diff view truncates any line wider than its pane with a trailing `…`
(`diffCell` → `truncate`). For long lines that hides the content. Horizontal
scroll was the obvious fix but is fiddly in a two-pane layout; **word-wrap**
is the friendlier answer and the one the user chose. A wrapped line spans
several display rows; in a side-by-side view the two sides must stay aligned
so a changed pair still reads as a pair.

## Scope

In scope:

- A `w` key toggling wrap in the diff view; default **off** (today's
  truncate-with-`…`), **session-remembered** via `Model.diffWrap` (same shape
  as `f`/`diffPartial`).
- Lockstep two-pane wrapping: a logical row is as tall as its taller side;
  gutter (line number) on the first display row only, blank on continuations;
  `│` separator on every display row; a gap cell (absent side of an add/delete)
  shows its `·` filler down all the block's display rows.
- Word-boundary breaking, hard-breaking a single word longer than the pane.
- Display-row scroll: `offset` indexes display rows; `↑/↓/pgup/pgdn` and the
  `rows X–Y/N` readout count display rows; `n`/`p` block jumps land on the
  change's first display row.
- Re-wrap + viewport re-anchor on terminal resize.
- Composes with full **and** partial modes, and with intraline emphasis.

Out of scope:

- Horizontal scroll (this replaces the need for it).
- Per-pane independent wrapping (alignment would break).
- Configurable wrap column / wrap-at-word-vs-character preference.
- Any change to `textdiff`, `domain`, `cache`, the loaders' fetching, or the
  cache key.

## Decisions (locked during brainstorming)

- **Display-row scroll** (not logical-line): `offset` and all navigation index
  the wrapped display stream; the viewport re-anchors (rather than holding an
  exact row) on resize/toggle.
- **`w` toggles**, default off, session-remembered.
- **Lockstep height**: a row occupies `max(leftSegs, rightSegs, 1)` display
  rows; a gap side fills with `·`, a present-but-shorter side fills with blank.
- **Word-break**, hard-break for an over-long word.
- Wrapping lives in the **TUI** (it needs display-width / sanitized text), not
  in pure `textdiff`.

## Components

### The three display stages

The view already expands twice; wrap adds a third stage keyed by width:

```
full / fullBlocks   ──mode (Expand/Collapse)──▶   lines / blocks
   (immutable)                                    (logical, mode-only)
                                  │
                                  └── relayout(width) ──▶   disp / dispBlocks
                                       (display rows; offset & nav index THESE)
```

- **wrap off** (or width unknown): `disp` is `lines` 1:1 (each `Line` → one
  `dRow`), `dispBlocks == blocks`. The renderer truncates a too-long row with
  `…` exactly as today → **byte-identical** output.
- **wrap on**: each row's two sides are sanitized+wrapped into ≤`paneW`
  segments; the row expands to `max(leftSegs, rightSegs, 1)` `dRow`s;
  `dispBlocks[i]` is the display-row index where `lines[blocks[i]]` begins.

### `diffView` new state

```go
// added to diffView:
wrap   bool   // current wrap mode (false = truncate, today's behavior)
width  int    // overlay width at last layout (0 = unset → wrap-off render)
disp       []dRow // display rows: what offset indexes and the renderer draws
dispBlocks []int  // change-block starts as display-row indices (jump targets)
lineStart  []int  // logical line index → its first display-row index (re-anchor + block remap)
```

`offset` (existing) now means "top visible **display** row". The existing
`lines []Line` / `blocks []int` stay as the logical (mode) stream that
`relayout` consumes.

```go
// dRow is one display row: a fold marker, or one (possibly continuation)
// slice of an aligned Row. line is the source index into v.lines (for
// resize re-anchoring).
type dRow struct {
	line int           // source logical-line index
	fold int           // >0: a fold separator of N unchanged lines (whole width)
	row  textdiff.Row  // the source aligned row (for Kind / gutter numbers / gap)
	left, right cellSeg // this display row's slice of each side
	first bool          // true on the row's first display line (gutter shows here)
}

// cellSeg is one pane's text for one display row: the sanitized runes and the
// parallel emphasis mask, already ≤ paneW wide. A zero cellSeg renders blank.
type cellSeg struct {
	disp []rune
	emph []bool
}
```

### `relayout(width int)` — build the display stream

```
v.width = width
paneW := (width-1)/2 (clamped ≥4, as diffPaneLines does); tw := text width per pane
reset v.disp, v.dispBlocks, v.lineStart (len = len(v.lines))
for li, ln := range v.lines:
    v.lineStart[li] = len(v.disp)
    if !v.wrap || width <= 0:
        // 1:1 whole-row dRow; the renderer draws it via the existing
        // diffCell(raw row) path (truncates with …) → byte-identical.
        v.disp = append(v.disp, dRow{line:li, fold:ln.Fold, row:ln.Row, first:true})
        continue
    if ln.Fold > 0:
        v.disp = append(v.disp, dRow{line:li, fold:ln.Fold, first:true}); continue
    // wrap on, aligned row:
    leftSegs  := wrapSide(ln.Row.Left,  ln.Row.LeftSpans,  ln.Row.Kind, left,  tw)
    rightSegs := wrapSide(ln.Row.Right, ln.Row.RightSpans, ln.Row.Kind, right, tw)
    h := max(len(leftSegs), len(rightSegs), 1)
    for k := 0; k < h; k++:
        v.disp = append(v.disp, dRow{line:li, row:ln.Row, first: k==0,
                                     left: segAt(leftSegs,k), right: segAt(rightSegs,k)})
v.dispBlocks = map each b in v.blocks → v.lineStart[b]
clamp v.offset into range
```

`wrapSide` returns `nil` for a gap side (Add's left / Del's right) so the
renderer draws `·` filler on every display row; otherwise it sanitizes via the
existing `sanitizeSpans` and splits into ≤`tw` segments.

### `wrapCells` — the splitter (pure, unit-tested)

```go
// wrapCells splits a sanitized (disp, emph) line into segments each ≤ tw
// display columns, breaking at the last space at/under the boundary; a word
// longer than tw is hard-broken at tw. Width is measured per rune with
// lipgloss.Width (control chars/tabs are already expanded by sanitizeSpans, so
// nearly all runes are width 1; CJK width is honored). Always returns ≥1
// segment (an empty line → one empty segment). The emph mask is carried
// alongside so emphasis survives a split.
func wrapCells(disp []rune, emph []bool, tw int) []cellSeg
```

### Navigation — retargeted, logic unchanged

`scroll`/`jumpTo`/`nextBlock`/`prevBlock`/`currentBlockOrdinal` already operate
on "the displayed stream + its blocks + offset". They are repointed from
`v.lines`/`v.blocks` to `v.disp`/`v.dispBlocks`. Because wrap-off makes `disp`
mirror `lines` 1:1 with `dispBlocks == blocks`, their behavior is identical to
today when wrap is off. `diffLead`/`diffContext` semantics are unchanged
(a jump still lands the change `diffLead` rows below the top — now display
rows).

### `rebuild()` and the `w` toggle

- `rebuild()` (mode change, `f`) recomputes `lines`/`blocks` (Expand/Collapse)
  then calls `relayout(v.width)`.
- **`w` handler** (in `updateDiffViewKey`): capture the current change ordinal
  (`currentBlockOrdinal`), flip `v.wrap`, set `m.diffWrap = v.wrap`,
  `relayout(v.width)`, then re-anchor to that change via
  `jumpTo(v.dispBlocks[ord], body)` (clamped), or `offset = 0` when there are
  no blocks — the exact pattern the `f` toggle already uses.

### Resize — re-anchor

In `model.go`'s `WindowSizeMsg` case, after the existing `<60` close guard, if
the diff view is still open: capture `topLine := v.disp[v.offset].line` (guard
empty `disp`), call `v.relayout(newOverlayWidth)`, set
`v.offset = v.lineStart[topLine]` then `v.scroll(0, m.diffBodyRows())` to clamp.
The viewport keeps its logical place across the re-wrap. (`newOverlayWidth` is
the diff overlay width derived from `msg.Width`, matching what the loaders pass.)

### Session flag + creation sites

`Model.diffWrap bool` (default false), beside `diffPartial`. Every `diffView`
construction sets `wrap: m.diffWrap` and `width:` the current overlay width, so
the first `relayout` (inside `rebuild` via the loaders' `applyDiff`) wraps
correctly without waiting for a resize. Sites: the two loaders
(`loadStatusDiffCmd`, `loadCommitDiffCmd`), the two loading-stub arms
(`model.go` status enter, `files_view.go` tree enter). The loaders already
capture `body`; they additionally capture the overlay width.

### Render reuse

`diffPaneLines` iterates `v.disp[offset:offset+body]` instead of
`v.lines[...]`. The renderer **branches on `v.wrap`**:

- `fold > 0` → `foldSeparator(fold, w)` (unchanged, one display row) — both modes.
- **wrap off** → the existing path verbatim: `diffCell(row.LeftNo, row.Left,
  …, row.LeftSpans) + "│" + diffCell(row.RightNo, row.Right, …)`. Truncates
  with `…`. **Byte-identical** to today (wrap-off `dRow`s carry no `cellSeg`).
- **wrap on** → `segCell(left) + "│" + segCell(right)` where `segCell` renders
  a pre-sanitized `cellSeg` (already ≤`tw`): gutter (number on `first`, blank
  otherwise) + styled body via the existing `styledRuns` + pad. A gap side
  (`wrapSide` returned nil) → `diffGapCell` `·` filler. A present-but-empty
  continuation segment → blank padded to `tw` in the side's base style.

`segCell` shares the gutter + `styledRuns` + pad logic that `hotEmphBody`
already uses (both consume `(disp, emph)`), so wrap-on segments and the
emphasis renderer agree; only the wrap-off path goes through raw-text
`diffCell`.

## Data flow — opening a diff with wrap on

1. Loader builds the `diffView` with `wrap: m.diffWrap`, `width:` overlay
   width, fetches the diff via the cached `Differ` (unchanged).
2. `applyDiff` sets `full`/`fullBlocks`, calls `rebuild()` → `lines`/`blocks`
   (mode) → `relayout(width)` → `disp`/`dispBlocks`, then
   `jumpTo(dispBlocks[0], body)` opens at the first change's first display row.
3. Render draws `disp[offset:offset+body]`.

## Edge cases

- **Gap side** (Add/Del): `wrapSide` returns nil → `·` filler on every display
  row of the block; the text side wraps to `h` rows; the gap side has no number.
- **Empty line** (e.g. a blank `Same` row): one segment, one display row.
- **Word longer than `tw`**: hard-broken at `tw` (no infinite loop; each
  segment consumes ≥1 rune).
- **Very narrow pane** (`tw` tiny / degenerate): `wrapCells` still emits ≥1-rune
  segments; the existing `gut > width-2` clamp in the cell renderer holds.
- **width unset (`0`)** — e.g. a directly-constructed test `diffView`:
  `relayout` falls back to 1:1 (wrap-off), so existing tests that build a
  `diffView` and call `rebuild()` are unaffected.
- **Binary / too-large / no-content / loading**: no panes drawn; wrap is inert.
- **Emphasis across a wrap boundary**: the `emph` mask is split with the runes,
  so a highlighted word that straddles a break stays highlighted on both
  segments.

## Error handling

No new failure modes — wrap is a pure layout transform over already-fetched
rows. A `relayout` with no lines yields empty `disp` (the render shows the
existing binary/too-large/no-content states). Resize re-anchor guards an empty
`disp`/out-of-range `offset`.

## Testing

Pure helpers:

- `wrapCells`: single short line → one segment; word-boundary break; hard-break
  of an over-long word; multi-break; emphasis mask carried across a split;
  empty input → one empty segment; CJK/wide-rune width honored.
- `relayout` (wrap on): lockstep height = `max(leftSegs, rightSegs)`; gutter
  number only on the `first` row; `dispBlocks` remap equals
  `lineStart[blocks[i]]`; a gap side produces filler rows; **wrap off ⇒
  `disp` mirrors `lines` 1:1 and `dispBlocks == blocks`**.

View level:

- `w` flips `v.wrap` and sets `m.diffWrap`; a new diff inherits the session flag.
- `n`/`p` land on a change's first display row when wrapped (assert the target
  block is within `[offset, offset+body)`).
- Resize re-anchors: open wrapped, scroll, shrink width, assert the same
  logical line is still at/near the top.
- **Every existing `diff_render_test.go` and `diff_view_test.go` test passes
  unchanged** — proof that wrap-off is byte-identical and navigation is
  preserved.

Hint + help: `diffHint` gains `[w] wrap`; `help.go`'s "Diff view" section gains
a `w` row. (The diff view draws its own hint line, so `TestHelpFooterCoverage`
is uninvolved — no footer-registry/avail change.)

## File structure

- Modify: `internal/tui/diff_view.go` — `diffView` fields (`wrap`, `width`,
  `disp`, `dispBlocks`, `lineStart`), `dRow`/`cellSeg` types, `relayout`,
  `rebuild` calls `relayout`, navigation retargeted to `disp`/`dispBlocks`,
  `w` key case, loaders set `wrap`/`width`.
- Modify: `internal/tui/diff_render.go` — `wrapCells`, `wrapSide`,
  `diffPaneLines` iterates `disp`, cell renderer factored to take a `cellSeg`,
  `diffHint` += `[w] wrap`.
- Modify: `internal/tui/model.go` — `Model.diffWrap`; `WindowSizeMsg`
  re-anchor; status enter-arm stub sets `wrap`/`width`.
- Modify: `internal/tui/files_view.go` — tree enter-arm stub sets `wrap`/`width`.
- Modify: `internal/tui/help.go` — "Diff view" `w` row.
- Modify: `internal/tui/diff_view_test.go`, `diff_render_test.go` — new tests;
  any `diffViewWith`-style helper updated to populate `disp` (via `rebuild`/
  `relayout`) so view tests exercise the display stream.
- Docs: `CHANGELOG.md` (always), `README.md` (the diff keymap row gains `[w]`).
  No `CLAUDE.md` change (no package-map/arch change). No CLI surface ⇒ no
  agentskill bump. adding-tui-windows skill unchanged (no new window kind).

## Success criteria

- `w` toggles word-wrap in the diff view; long lines wrap across display rows
  with the two panes row-aligned, gutter on the first row, gap filler on absent
  sides.
- Wrap is default-off and session-remembered; opening a diff inherits it.
- Display-row scroll, `n`/`p`, and the `rows X–Y/N` readout are correct when
  wrapped; resize re-wraps and keeps the viewport's place.
- Intraline emphasis survives wrapping.
- Wrap-off rendering is byte-identical to today (all existing diff tests pass).
- `./test.sh race` is green.
