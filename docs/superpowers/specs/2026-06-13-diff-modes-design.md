# Diff view: partial/full modes + change navigation — design

**Date:** 2026-06-13 · **Branch:** `feat/diff-modes`

## Goal

Three additions to the existing full-screen side-by-side diff viewer
(shipped at `ea45f08`):

1. A **GitHub-style partial mode** that shows only change blocks plus a few
   context lines, collapsing each long unchanged run into one fold marker —
   alongside the existing **full** mode, with a key to toggle.
2. **`n` / `p`** as alternatives to `ctrl+↓` / `ctrl+↑` for next/previous
   change jumps.
3. On open, the view is **scrolled to the first difference** instead of the
   top.

## Decisions (brainstormed)

- **Default mode:** full. The toggle is **remembered for the session** — once
  you switch to partial, subsequent diffs open partial too (a single
  `Model.diffPartial` flag, default `false`).
- **Toggle key:** `f` (mnemonic: full/partial).
- **Context lines:** 3, fixed (git's default, what GitHub shows). No config
  knob until one is missed (YAGNI).

## Architecture

```
textdiff.Compare ──▶ Result{Rows, Blocks}     (unchanged)
                          │
        full mode: wrap rows 1:1 as Lines
        partial mode: textdiff.Collapse(rows, blocks, 3) ──▶ Lines + remapped Blocks
                          │
                  diffView.lines / diffView.blocks   (what offset indexes, what renders)
```

The collapse is a **pure transform in `internal/textdiff`** — boundary and
block-merge logic is off-by-one-prone, so it belongs where it is
table-testable, and it keeps the renderer thin. The fold marker's *rendered
string* stays in the TUI.

## Components

### 1. `internal/textdiff`: `Line` + `Collapse`

```go
// Line is one display line of a diff view: either an aligned Row (Fold == 0)
// or a fold marker standing in for Fold elided equal rows (Row is zero).
type Line struct {
    Row  Row
    Fold int // > 0: this line hides Fold unchanged rows
}

// Expand wraps every row as a Line 1:1 (full mode): no folds.
func Expand(rows []Row) []Line

// Collapse produces the partial (GitHub-style) view: every change block plus
// up to `context` equal rows on each side is kept; each remaining run of
// equal rows becomes a single fold Line. It returns the kept Lines and the
// block-start indices remapped into that Line slice (for jump navigation).
// context < 0 is treated as 0.
func Collapse(rows []Row, blocks []int, context int) (lines []Line, blockIdx []int)
```

`Collapse` algorithm: mark every row within `context` of any non-`Same` row
as *kept*; emit kept rows in order; each maximal run of unkept (equal) rows
between kept regions — and any leading/trailing unkept run — becomes one
`Line{Fold: runLength}`. Adjacent change blocks whose context windows
overlap or touch merge into one kept region with no fold between them (so two
nearby changes read as one hunk, like git). `blockIdx[i]` is the index in
`lines` of the first row of the i-th block (a row Line, never a fold).

Edge cases: no blocks → `lines` empty, `blockIdx` empty (the caller's
"(no content difference)" note covers it); a change at the very start/end →
no leading/trailing fold there; `context` larger than a gap → no fold for
that gap (everything kept). `Expand` and `Collapse(…, ≥len)` agree on row
content; only folds differ.

### 2. `internal/tui`: mode state, rendering, navigation

#### `diffView` (`diff_view.go`)

```go
type diffView struct {
    title, context string
    full      []textdiff.Row // immutable aligned rows (the comparison result)
    fullBlocks []int         // immutable block starts into full
    partial   bool           // current display mode (false = full)
    lines     []textdiff.Line // displayed sequence for the current mode
    blocks    []int           // block-start indices into lines (jump targets)
    offset    int             // top visible line
    truncated bool
    binary, tooLarge, loading bool
    err error
}
```

- `fillDiff` now stores `full`/`fullBlocks` from `Compare`, then calls
  `rebuild()` which sets `lines`/`blocks` from the current `partial`:
  full → `Expand(full)` + `fullBlocks`; partial → `Collapse(full, fullBlocks, diffContext)`.
  `diffContext = 3` is a package const.
- `scroll` clamps against `len(v.lines)` (was `len(v.rows)`).
- The loader sets `v.partial = m.diffPartial` (captured at open) before
  `fillDiff`, then sets the **open offset** to the first difference via
  `v.jumpTo(firstBlock, body)` (body captured from `m.diffBodyRows()` at open).

#### Navigation — one rule for jumps and open

```go
// diffLead is the number of context lines kept above a jumped-to change.
const diffLead = 3

// jumpTo positions block-start line `b` with up to diffLead lines above it,
// clamped to the scroll range [0, len(lines)-body] (same clamp as scroll).
func (v *diffView) jumpTo(b, body int) {
    v.offset = max(0, b-diffLead)
    v.scroll(0, body) // reuse scroll's clamp
}
```

(`scroll` is the existing delta+clamp helper, now clamping against
`len(v.lines)`; `jumpTo` sets an absolute offset then lets it clamp.)

- **Open:** `jumpTo(blocks[0], body)` when blocks exist, else offset 0.
- **`n` / `ctrl+down`** (next): first block `b` with `b > v.offset + diffLead`
  → `jumpTo(b)`. The `+diffLead` neutralizes the lead so the current change
  isn't re-selected. The existing progress-aware end behavior is preserved:
  if the only remaining block can't advance the clamped offset, it's a no-op.
- **`p` / `ctrl+up`** (prev): largest block `b` with `b < v.offset + diffLead`
  → `jumpTo(b)`.

This **changes the existing `ctrl+↑/↓` landing**: today they put the change on
the very top line; under the unified rule the change lands with up to 3
context lines above it. Intentional, consistent improvement — it updates the
exact-offset assertions in `TestDiffViewKeysScrollAndJump` and
`TestDiffViewJumpAtMaxScrollIsNoOp`.

#### Toggle (`f`)

```go
case "f":
    // Preserve which change you're looking at across the mode switch, by
    // block ordinal (mode-independent). Rebuild lines/blocks, re-jump.
    ord := v.currentBlockOrdinal()      // # blocks with start ≤ offset+diffLead, minus 1, clamped ≥ 0
    v.partial = !v.partial
    v.rebuild()
    m.diffPartial = v.partial            // remember for the next diff this session
    if len(v.blocks) > 0 {
        v.jumpTo(v.blocks[min(ord, len(v.blocks)-1)], m.diffBodyRows())
    } else {
        v.offset = 0
    }
```

Toggling on binary/tooLarge/loading/error views flips `partial` + the session
flag but changes nothing visible (those bodies ignore the line stream) —
harmless, no special-case.

#### `n` / `p` / `f` key routing

`updateDiffViewKey` gains `case "n"` (= ctrl+down body), `case "p"` (= ctrl+up
body), and `case "f"`. `ctrl+up`/`ctrl+down` keep working (shared helpers).
All other keys still swallowed.

#### Rendering (`diff_render.go`)

- `diffPaneLines` iterates `v.lines` (not `v.rows`); a row Line renders via the
  existing `diffCell` path, a fold Line (`Fold > 0`) renders as **one
  full-width dim separator**: a centered label `⤬ N unchanged lines` padded
  with `─` to the terminal width (single style, `diffGapCell`/a new dim
  style). The label is singular for `N == 1`.
- The line-number **gutter width** is computed from `v.full` (immutable), so
  columns don't shift when you toggle.
- The header range (`rows a–b/N`) uses `len(v.lines)` as `N` (the scrollable
  extent in the current mode).

#### Open offset wiring

Both open sites (`model.go` status enter arm, `files_view.go` tree enter arm)
already build a `loading` stub `&diffView{…, loading:true}`; they additionally
set `partial: m.diffPartial` on the stub (so a toggle during load is
coherent), and the loader carries the mode + computes the open offset as
above. `reRoot` already nils the view; `m.diffPartial` persists across reRoot
(a session preference, not repo state) — intentional.

### 3. `Model` (`model.go`)

One new field: `diffPartial bool` (session default, `false` = full), read when
a diff opens and written by the `f` toggle. No other Model changes.

## Hint line & help

- The diff view's own hint line becomes:
  `[↑↓] scroll  [n/p] prev/next change  [f] full/partial  [esc] close  [q] quit`
  (`ctrl+↑/↓` still work; the hint favors the shorter `n/p`).
- `help.go` "Diff view" section: add `n/p` (alias of `ctrl+↑/↓`), `f`
  (toggle full/partial), keep the `ctrl+↑/↓` row. No registry/footer change —
  the diff view draws its own hint line, so `TestHelpFooterCoverage` is not
  involved.

## Testing

- `internal/textdiff`: table tests for `Collapse` — single block mid-file
  (leading + trailing fold, correct fold counts, kept context = 3 each side);
  change at file start (no leading fold) and end (no trailing fold); two
  blocks far apart (fold between); two blocks within 2·context (windows
  touch → merge, no fold between); `context` ≥ gap (no fold); block-index
  remap correct (each `blockIdx` points at a row Line, never a fold); no
  blocks → empty. `Expand` round-trips row content. Property check:
  `Collapse(rows, blocks, n)` with `n ≥ len(rows)` keeps every row (folds
  only collapse runs strictly longer than the kept windows).
- `internal/tui`:
  - `rebuild`/mode: full mode lines == Expand; partial mode folds a long
    gap; gutter width identical across modes for the same diff.
  - open offset = first difference (full: `max(0, blocks[0]-3)`; a no-change
    diff opens at 0).
  - `n`/`p` == `ctrl+down`/`ctrl+up` (same resulting offset from the same
    start); next/prev land with 3 context lines above; prev from mid-scroll
    snaps to the block above; end-of-list `n` is a no-op (progress-aware).
  - `f` toggles the view AND sets `m.diffPartial`; a second diff opened after
    a toggle inherits partial; ordinal preserved across toggle (same change
    block in view before/after).
  - render: fold separator appears in partial mode and not in full; full-width;
    `rows a–b/N` uses the displayed line count.
  - the updated exact-offset jump tests reflect the `-3` lead rule.
- No e2e (TUI-only).

## Out of scope (unchanged from the diff-view spec)

Intraline word emphasis, horizontal scroll, staged-vs-unstaged split,
configurable context count, per-file mode memory (the session flag is the
only memory).
