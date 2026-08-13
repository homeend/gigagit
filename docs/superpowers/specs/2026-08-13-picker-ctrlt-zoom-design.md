# Hunk-picker ctrl+t zoom — design

**Date:** 2026-08-13
**Status:** approved (user confirmed both interaction decisions)

## Problem

The hunk picker splits its body into the selection grid (top) and the
assembled-output pane (bottom, ~1/3 of the body). On small terminals, or when
reading a long assembled result, either half can be too short. There is no way
to temporarily give one half the whole screen.

ctrl+t is the established "fullscreen the focused thing" chord everywhere else
in the TUI (panels via `fullMaxed`, popups via `popupMax`). On the picker —
a full-screen surface, not a popup — ctrl+t currently falls through to the
picker's own `update` and is a **no-op**. This feature gives it the analogous
meaning inside the picker.

## Behavior

ctrl+t toggles a **zoom**: the half that currently has tab-focus occupies the
entire picker body.

- **Grid focused** (`!outFocused`): the grid renders at full `bodyH`; the
  output rule and pane are not rendered. Visually like `o`-collapsed, but
  temporary — `outCollapsed`, `oshift`, and focus are untouched.
- **Output focused** (`outFocused`): the rule (with its `▶` focus marker) plus
  the output pane render at full `bodyH`; the grid and column-labels row are
  not rendered.
- ctrl+t again restores the normal split.

The zoomed half is **not stored** — zoom is a single `zoomed bool` on
`hunkPicker`, and the rendered half is always the focused one. Consequences:

- **Tab under zoom switches focus as usual, and the fullscreen swaps to the
  newly focused half** (user decision: zoom follows focus, matching the main
  screen where a fullscreen pin follows tab). Tab's existing behavior is
  unchanged (expanding a collapsed pane, resetting `oshift` on leave, etc.).
- **Gated enter** (pending regions) already refocuses the grid
  (`outFocused=false`); under zoom this coherently shows the first undecided
  region full-screen. No special-casing.

### Key handling under zoom

- `ctrl+t` — toggle `zoomed`. Handled in the picker's `update` (the central
  handler in model.go only serves `maximizableLayer` popups; surfaces fall
  through).
- `esc` — **restores first** (user decision): while zoomed, esc sets
  `zoomed = false` and is consumed; the next esc cancels the picker as today.
- `o` — drops the zoom, then applies its normal meaning. Grid-focused: pane
  collapses (already hidden visually; now stays hidden after unzoom).
  Output-focused: pane collapses and focus returns to the grid with `oshift`
  reset (existing `o` behavior).
- Everything else (space, c/i/C/I, arrows, alt+↑/↓, shift+←/→, z, n/p,
  enter) keeps its current meaning, operating within the zoomed half. The
  existing outFocused routing already makes selection keys inert while the
  output is focused.

Zoom state lives on the picker instance and dies with it; reopening the picker
starts unzoomed.

## Rendering

In `render`, when `zoomed`:

- skip the `outH = bodyH/3` split math entirely;
- grid-focused: `gridH = bodyH`, `outH = 0` (column labels row kept);
- output-focused: the rule row **replaces** the column-labels row, so the line
  layout stays header / rule / `renderOutput(w, bodyH)` / blank / hints — the
  pane gets the full `bodyH`; no column labels, no grid rows.

`vshift`/`oshift` clamp to whatever height they are given (clamp +
store-back), so no scroll changes are needed.

Hint rows: add `[ctrl+t] full` to **both** hint sets (grid-focused and
output-focused). Help (`help.go`) gets the picker ctrl+t mention. New i18n
keys go into all four bundles (ja/ko/zh/ru) — AST-gate enforced.

## Out of scope

- Persisting zoom across picker instances or sessions.
- The sub-10-row `outH==0` tab-focus edge (parked previously; zoom actually
  gives such terminals a usable output view).
- Web frontend (sub-project 3 handles web parity).

## Testing

TDD in `internal/tui` (drive `update(keyMsg(...))` + `ansi.Strip(render(...))`):

1. Render: zoomed grid shows no rule/output lines and more grid rows than the
   split view; zoomed output shows the rule + output lines and no grid rows.
2. ctrl+t toggles: zoom on, zoom off restores the split.
3. Tab under zoom swaps which half is fullscreen (focus marker moves, rendered
   content switches).
4. Esc under zoom restores the split and does NOT close the picker; esc again
   cancels.
5. `o` under zoom drops zoom and collapses the pane (both focus cases).
6. Existing picker suite stays green.
