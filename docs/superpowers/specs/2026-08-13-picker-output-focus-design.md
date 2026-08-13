# Hunk picker: Tab focus + scrollable output pane

**Date:** 2026-08-13
**Status:** Approved design
**Surface:** `internal/tui/conflict_picker.go` (+ help/i18n/docs)

## Problem

The picker's output pane always auto-follows the grid cursor and cannot be
scrolled: with a long assembled result only the window around the focused
region is ever visible. `alt+↑/↓` free-scrolls the grid, but nothing
scrolls the output.

## Feature

- **Tab toggles focus** between the **grid** (the left/right input
  columns) and the **output pane**. Tab while the pane is collapsed
  expands it AND focuses it in one press.
- **Output focused:** plain `↑`/`↓` (and `k`/`j`) scroll the output one
  line per press, end to end. The selection keys (`space`, `c`, `i`, `C`,
  `I`, `n`, `p`, `←`, `→`) are **inert** until focus returns to the grid.
  `esc`/`enter` keep their global meanings (cancel / save), `z` and
  `shift+←/→` keep changing the shared display mode / hscroll, `alt+↑/↓`
  keeps free-scrolling the grid view, and `o` collapses the pane and
  returns focus to the grid.
- **Tab back to the grid** (or collapsing the pane): the manual output
  scroll is discarded and the pane resumes auto-following the grid cursor.
  Grid behavior under grid focus is exactly today's.
- **Focus is always visible:** the `── output ──` rule carries a focus
  marker and highlight while the pane is focused.
- **Hints adapt to focus:** grid focus shows today's hint row plus
  `[tab] output`; output focus shows a short row —
  `[↑/↓] scroll`, `[tab] grid`, `[o] hide`, `[z] mode`,
  `[shift+←/→] scroll`, `[alt+↑/↓] view`, `[enter] apply`,
  `[esc] cancel`.
- Both flavors (conflict resolve and `H` staging) share the surface and
  the behavior.

## Design

Mirror the proven `vshift` pattern — no new windowing machinery:

- `hunkPicker` gains `outFocused bool` and `oshift int`. `oshift` is a
  **display-line delta from the follow-anchor window start**, applied and
  clamped inside `renderOutput` exactly like `vshift` is in
  `renderTwoCol`: `start = clamp(windowStart(...) + oshift, 0,
  max(0, len(dl)-h))`, and the **effective (clamped) delta is stored
  back** (`render` has a pointer receiver), so held scrolling never
  accumulates invisible drift at either end. "Resume following" is
  `oshift = 0`.
- Key routing in `update`, in order after the ctrl+c check:
  1. `tab`: if the pane is collapsed → `outCollapsed = false`,
     `outFocused = true`; else toggle `outFocused`. Whenever focus
     LEAVES the output (toggle to grid, or `o` collapse below),
     `oshift = 0`.
  2. If `outFocused`: `up`/`k` → `oshift--`, `down`/`j` → `oshift++`,
     `o` → `outCollapsed = true; outFocused = false; oshift = 0` —
     each returns consumed. `esc`, `enter`, `z`, `shift+left`,
     `shift+right`, `alt+up`, `alt+down` fall through to their existing
     handling. **Every other key returns consumed** (inert).
  3. The existing alt/vshift pre-switch and the main switch run
     unchanged (only reachable under grid focus for cursor/pick keys).
- `render`: the rule line highlights when focused (marker + emphasis
  style vs today's dim); the hint list is chosen by `outFocused`.
  `renderOutput` gains the oshift window arithmetic + store-back.
- Collapsing via `o` under GRID focus behaves as today (`outFocused` is
  already false). `outFocused` is only reachable with the pane visible.

### i18n

New literal keys in all four bundles (ja/ko/zh/ru): `[tab] output`,
`[tab] grid`, `[↑/↓] scroll`, `[o] hide`, and the help rows for Tab/output
focus. Existing keys are untouched.

## Out of scope

- Page-size jumps, home/end in the output, mouse focus/scroll.
- Persisting focus or scroll across picker sessions.
- Any change to the grid's own key semantics under grid focus.

## Testing

`internal/tui/conflict_picker_test.go`, existing style:

1. Tab toggles `outFocused`; Tab on a collapsed pane expands AND focuses;
   `o` under output focus collapses, returns focus to the grid, and
   resets `oshift`.
2. Output focused: `↓` scrolls the pane window (distinct-line fixture —
   assert the rendered pane content shifts) while the grid cursor
   (`bi`/`line`/`side`) and all picks stay frozen; `c`/`space`/`n` are
   inert.
3. Tab back resets `oshift` (pane resumes following the cursor).
4. `esc` still cancels and `enter` still gates/applies from output focus.
5. Clamp + store-back: large `oshift` in both directions pins to the
   ends and never panics; the stored value is the effective one.
6. Grid focus: today's tests keep passing unchanged; hint row contains
   `[tab] output` under grid focus and `[↑/↓] scroll` + `[tab] grid`
   under output focus. i18n AST gates cover the new keys.
