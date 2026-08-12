# Free view-scroll in the hunk picker (conflict resolve / hunk staging)

**Date:** 2026-08-13
**Status:** Approved design
**Surface:** `internal/tui/conflict_picker.go`, `internal/tui/twocol.go`

## Problem

The hunk picker (the two-column current/incoming screen used for manual
conflict resolution, and shared by hunk staging) windows its content purely
around the cursor row: `renderTwoCol` anchors the viewport to the selected
line and there is no way to look elsewhere. When a block is in line-by-line
mode, the computed `result:` preview rendered below the block can sit
off-screen with no way to reach it — the user cannot inspect what their
picks produce without moving the selection.

## Feature

A free vertical view-scroll, independent of the selection cursor:

- **Alt+↑ / Alt+↓** move the viewport up/down **one display line** per
  keypress, without moving the cursor or changing any picks.
- The first plain **↑/↓** (or `k`/`j`) after a free-scroll **snaps the
  viewport back** to the cursor and is otherwise **consumed** — the cursor
  does not move on that keypress. Subsequent presses move the cursor as
  today.
- Any other key that moves the cursor, edits picks, or reshapes the
  display (`n`, `p`, `←`, `→`, `space`, `c`, `i`, `C`, `I`, `z`,
  `shift+←/→`) resets the scroll and performs its normal action.
  `enter`/`esc` behave as today.

Both picker flavors get the feature (the surface is shared):
conflict resolution (`newConflictPicker`/`newProcessConflictPicker`) and
hunk staging (`newStagePicker`).

## Design

Represent the free scroll as a **delta from the anchored window start**,
not an absolute position. The offset stays meaningful when rows re-render
(wrap-mode toggle, a pick adding/removing `result:` preview lines), and
snap-back is simply `delta = 0`. All display-line (wrap-aware) math stays
inside `twocol.go`, which is the only place display lines exist.

### `internal/tui/twocol.go`

- `twoColOpts` gains `vshift int` — a display-line delta applied after the
  anchored window start is computed:
  `start = clamp(windowStart(...) + vshift, 0, max(0, len(dl)-h))`.
- `renderTwoCol` additionally returns the **effective (clamped) shift**
  (`start - anchoredStart`) so the caller can store it back — otherwise a
  held Alt+↓ at the bottom accumulates invisible drift and Alt+↑ appears
  dead until the drift unwinds.
- `vshift == 0` is exactly today's behavior.

### `internal/tui/conflict_picker.go`

- `hunkPicker` gains `vshift int`, passed through to `renderTwoCol`.
- Key handling in `update`:
  - `alt+up` → `vshift--`; `alt+down` → `vshift++`. No clamping in the
    picker — the renderer clamps and `render` (pointer receiver) stores the
    effective shift back into `e.vshift`, so the value never drifts past
    the content bounds.
  - `up`/`k`/`down`/`j`: if `vshift != 0` → set `vshift = 0` and return
    (keystroke consumed). Else move the cursor as today.
  - `left`, `right`, `n`, `p`, `space`, `c`, `i`, `C`, `I`, `z`,
    `shift+left`, `shift+right`: set `vshift = 0`, then act as today.
  - `enter`, `esc`, `ctrl+c`: unchanged.
- Hint row gains `[alt+↑/↓] view` — a literal `i18n.T` key with entries in
  all four bundles (ja/ko/zh/ru); the AST-gate tests enforce this.

### Out of scope

- Page-size jumps (Alt+PgUp/PgDn) — one line per keypress only.
- Any change to the mouse, other pickers, or the diff view.
- Persisting the scroll across picker sessions.

### Caveat

Some terminals/tmux configurations swallow or remap Alt+arrow keys;
bubbletea reports them as `alt+up`/`alt+down` when they arrive. No fallback
binding is added in this iteration.

## Testing

Follow the existing style in `conflict_picker_test.go` (real key msgs
against a built picker; render assertions on plain-text output):

1. Alt+↓/Alt+↑ shifts the rendered window while cursor, side, and picks are
   unchanged.
2. With `vshift != 0`, a plain `↓` resets the window to the cursor and does
   NOT move the cursor; the next `↓` moves the cursor.
3. A pick key (`space`/`c`/`i`) and block-nav (`n`/`p`) reset `vshift` and
   still perform their action.
4. `renderTwoCol` clamps: large positive/negative `vshift` pins the window
   to the last/first page, returns the effective shift, and never panics
   on short content.
5. Hint row contains the new binding (covered implicitly by the i18n
   AST-gate tests for the bundles).
