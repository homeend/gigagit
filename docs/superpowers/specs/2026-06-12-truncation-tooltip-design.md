# Truncation tooltip — design

Status: approved in chat (2026-06-12). Branch: `feat/tui-tooltip`.

## Goal

When the selected row in the focused panel is too wide for the panel and gets
ellipsized (`/mnt/t/othe…`), the TUI shows the **full row text** in a floating
tooltip directly above the row — automatically, with zero extra keystrokes.
Selection is the keyboard TUI's "hover".

## Decisions (user-confirmed)

- **Trigger:** automatic, whenever the focused panel's selected row is
  truncated. Moving the selection moves/hides the tooltip.
- **Scope:** all list panels — Branches, Worktrees, Status, Commits.
- Tooltip is suppressed while a modal or any popup is open (those surfaces
  own the screen).

## UX contract

- The tooltip is a borderless one-to-three-line strip with a distinct
  background style (its own lipgloss style, analogous to `selectedRow` but
  visually different from it), showing the row's full text.
- Position: starts at the focused panel's content x; the strip's last line
  sits on the screen line **directly above** the selected row. If there is no
  room above (would start before screen row 0), it flips to directly
  **below** the row instead.
- Width: at most the terminal width (clamped right). Text longer than one
  line wraps (ANSI-/display-width-aware) up to **3 lines**; anything longer
  re-truncates the last line with `…`.
- No tooltip when the row fits, when the panel has no rows, or when a
  modal/popup is open. Filter-input mode keeps tooltips (selection is live).

## Architecture

Three small units, all in `internal/tui`:

1. **Geometry** (`viewstate.go`): `layout()` — already the single source of
   truth for panel *sizes* — additionally exposes each panel's **origin**
   (top-left x,y on screen). The selected row's screen line is
   `originY + 1 (border) + 1 (label) + selInWin`, where `selInWin` comes from
   the same `windowRows` call the renderer uses. Render and tooltip cannot
   drift because they share both sources.
2. **Compositor** (`view.go`): generalize `overlayCenter` into
   `overlayAt(bg, fg, left, top, termW, termH)`; `overlayCenter` becomes a
   thin wrapper that computes centered coordinates (pure refactor — existing
   popup behavior and tests unchanged).
3. **Tooltip** (new file `internal/tui/tooltip.go`): a `Model` helper that
   returns (text lines, x, y, ok). `render()` composites it over the
   interface only in the plain state (`modal == nil` and no popup pointers
   set).

Data flow in `render()`:

```
bg = renderInterface()
if no modal/popup:
    if tip, x, y, ok = m.tooltip(); ok:
        bg = overlayAt(bg, tip, x, y, w, h)
return bg (popups/modal handled as today)
```

The tooltip helper reads the focused panel's rows through `panelView`
(sort/filter respected automatically), rebuilds the same `prefix+row` string
the renderer truncates, and compares display width against the panel's inner
width from `layout()`.

## Edge cases

- Narrow-terminal (<40 cols) single-commits-panel mode: works the same; the
  commits panel has an origin there too.
- Selection at the top of the screen: flip-below rule.
- Wide glyphs: all measuring/wrapping uses display-width-aware helpers
  (`lipgloss.Width` / `ansi`-package functions), consistent with `truncate`.
- The tooltip may cover the panel label or the row above — acceptable and
  intended (it is an overlay, not a layout change).

## Testing (TDD, `internal/tui`)

- Long worktree path at narrow width → rendered output contains the full
  path; the tooltip's lines sit directly above the selected row's line.
- Flip-below when the selected row is the top screen line.
- No tooltip when: the row fits; a popup is open; a modal is open.
- Wrap: a row wider than the terminal wraps to ≤3 lines; a pathological row
  ends with `…`.
- Existing fit-test invariant (output never exceeds width×height) extended to
  tooltip states.
- `overlayAt` refactor: existing popup/overlay tests must pass unchanged.

## Non-goals

- No mouse hover, no delay timers, no per-panel configuration.
- No horizontal scrolling of rows.
- No tooltip for non-selected rows or unfocused panels.
