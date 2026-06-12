# Mouse focus: click-to-focus + wheel-over-panel — design

Date: 2026-06-13
Status: approved

## What

1. **Click-to-focus + select**: left-clicking a window focuses it and moves
   its selection to the clicked row. Works on the four normal panels and on
   both sides of the commit files view (tree click moves the tree cursor;
   commits click moves the commit selection through the follow-live path).
2. **Wheel-over-panel scrolling**: a wheel tick over a panel moves THAT
   panel's selection — position-targeted, focus untouched (the ctrl+↑/↓
   property: acting without owning focus). In the files view the wheel
   scrolls whatever is under the cursor; `ctrl+↑/↓` stays the
   always-the-tree key.
3. **Config entry `[ui] wheel_step`** (default 3) governing every wheel
   step, resolved through the existing defaults→global→repo overlay.
4. **New project skill** `.claude/skills/adding-config-entries/` documenting
   the config system and the checklist for adding an entry.

TUI + config only; no engine/CLI-surface/agent-skill changes.

## Approach (decided)

Pure hit-testing against `layout()` — the same `layoutGeom` (`pos`, `boxH`,
`leftW`/`rightW`) the renderer draws with — plus the `windowRows` start
offset extracted into a shared `windowStart(total, cap, sel) int` so the
renderer and the hit-test cannot disagree about which row is on which
screen line (the same single-source-of-truth rule layout()/panelView
already follow).

Rejected: a render-time region map (stateful, couples render to input,
awkward with the value-receiver Model) and the `bubblezone` dependency
(overkill for four rectangles).

## Hit-testing helpers (`internal/tui/viewstate.go`)

```go
// panelAt returns the panel whose box contains screen cell (x, y) under the
// current layout; ok is false on the header/footer/status rows or gaps.
func (m Model) panelAt(x, y int) (panel, bool)
```

A panel's rect: `pos[p].x ≤ x < pos[p].x + width(p)` and
`pos[p].y ≤ y < pos[p].y + boxH[p]`, where width is `leftW` for the three
left panels and `rightW` for Commits. Panels absent from `boxH` (hidden by
the layout) never match. Narrow terminals work for free (`boxH` only has
Commits there).

```go
// panelRowAt maps screen row y inside panel p to an index into p's DISPLAY
// rows (panelView order); ok is false on the border, the label line, or
// padding below the last row.
func (m Model) panelRowAt(p panel, y int) (int, bool)
```

Data rows start 2 lines below the box top (top border + label line):
`idx = windowStart(len(rows), rowsCap, sel) + (y - pos[p].y - 2)`, valid
only for `0 ≤ idx < len(rows)`. `windowStart` is the extracted start-offset
math of `windowRows` (refactor: `windowRows` calls it; behavior identical).

The files view has its own Y math in files_view.go (border + title line,
window over the FILTERED `visible()` list with `filesPageRows()` capacity).

## Mouse handling (`tea.MouseMsg` case in model.go)

Precedence, restructured:

1. **Help window open** (`contentPopup != nil`): wheel scrolls it (today's
   behavior, now stepping by `wheelStep()`); clicks ignored.
2. **Any other popup or the modal open**: ALL mouse input ignored (centered
   overlays — hit-testing the background would act on hidden state).
3. **Files view open**:
   - Left click in the left column: `filesTreeFocused = true`; on a visible
     tree line, also move the tree cursor to it (title/hint/padding: focus
     only).
   - Left click on the Commits panel: `filesTreeFocused = false`; on a data
     row, move the commit selection via `moveCommitUnderFilesView(delta to
     the clicked display row)` — clamps, dedupes by hash, fires at most one
     follow-live reload. Border/label clicks: focus only.
   - Wheel over the left column: tree `p.move(±wheelStep())`.
   - Wheel over the Commits panel: `moveCommitUnderFilesView(±wheelStep())`
     (follow-live, deduped).
   - Wheel/click elsewhere (header/footer): no-op.
4. **Normal mode**:
   - Left click on a panel: focus it — recording `lastLeftPanel` via the
     existing `rememberLeftFocus()` when leaving a left panel (same rule as
     `→`/tab) — and, when the click lands on a data row, set
     `m.sel[p]` to that display index. Clicks on border/label/padding focus
     without selecting.
   - Wheel over a panel: move THAT panel's selection by `wheelStep()`,
     clamped to `[0, panelLen-1]`. Focus unchanged.
   - Click/wheel outside any panel: no-op.

Click-to-focus and wheel are ungated on running/loading (pure
focus/selection movement, like the arrow keys); action keys remain
keyboard-only, so a selection moved mid-operation is inert.

Only `tea.MouseButtonLeft` presses act; wheel is
`MouseButtonWheelUp/Down` with `MouseActionPress` (as today). Right/middle
buttons, drag, release events: ignored.

## Config: `[ui] wheel_step`

`internal/config/config.go`:

```go
// UIConfig configures TUI behavior. TOML keys are snake_case.
type UIConfig struct {
	WheelStep int `toml:"wheel_step"` // rows per mouse-wheel tick; 0 = unset
}
```

- `Config` gains `UI UIConfig \`toml:"ui"\``.
- `Defaults()`: `UI: UIConfig{WheelStep: 3}`.
- `overlayUI(dst, src)`: `if src.WheelStep > 0 { dst.WheelStep = src.WheelStep }`
  — 0 means unset, matching the existing "zero value is unset; a higher
  layer cannot reset a lower layer's field" semantics. Negative values are
  likewise ignored.
- `Load` applies `overlayUI` alongside `overlayWorktree` for both layers.

TUI consumption — `m.wheelStep()` helper in the tui package:

```go
// wheelStep is the configured rows-per-wheel-tick, defaulting to 3 before
// the first config load (m.cfg is zero until dataLoadedMsg).
func (m Model) wheelStep() int {
	if s := m.cfg.UI.WheelStep; s > 0 {
		return s
	}
	return 3
}
```

The `contentWheelStep` const is REPLACED by `wheelStep()` everywhere
(help-window wheel, files-tree wheel, the new panel wheel) — one knob for
all wheel scrolling. `contentFastStep` (ctrl+↑/↓ in the help window) is
untouched.

## New project skill: `.claude/skills/adding-config-entries/SKILL.md`

Contents (written for an agent adding the NEXT config entry):

- The three layers and their paths: built-in `Defaults()` → global
  `$XDG_CONFIG_HOME/gg/config.toml` (fallback `~/.config/gg/config.toml`,
  see `DefaultGlobalPath`) → committed `<repo-top>/.gg.toml`. Repo wins.
- Field-level overlay semantics: each section has an `overlay<Section>`
  func copying only SET fields (non-empty string / non-empty slice /
  positive int); zero = unset, so a higher layer can never reset a field to
  the zero value — intentional, documented in `overlayWorktree`.
- Checklist for a new entry: (1) add the field to the right section struct
  (snake_case `toml` tag) or create a new section struct + `Config` field +
  `overlay<Section>` + wire it in `Load`; (2) set the default in
  `Defaults()`; (3) extend the overlay func; (4) table-test default /
  global-only / repo-only / repo-over-global in `config_test.go`; (5)
  consume it — TUI: `m.cfg` (loaded in `loadCmd`, arrives via
  `dataLoadedMsg`, zero before the first load → guard with a fallback
  helper like `wheelStep()`); CLI: `config.Load(config.DefaultGlobalPath(),
  filepath.Join(top, ".gg.toml"))` (see `internal/cli/worktree.go`); (6)
  document it in README's Configuration section; (7) the e2e harness pins
  its own `.gg.toml` — extend it only if scenarios need the new entry.
- `wheel_step` as the worked example. Local mutable state (`<seq>`
  counters) is NOT config — it lives in `<git-common-dir>/gg/state.toml`
  via `state.go`.

## Docs

- Help window, Global section: `r("click", "focus the window under the
  cursor and select the clicked row")` and `r("wheel", "scroll the list
  under the cursor (files view: tree or commits)")`. The footer registry is
  unchanged (mouse isn't a key binding; drift guard unaffected).
- README: Configuration section documents `[ui] wheel_step`; the key table
  gains a mouse row.
- CHANGELOG: new "Mouse focus & wheel" subsection.

## Testing

- `internal/config`: `wheel_step` default / global-only / repo-only /
  repo-over-global / zero-and-negative-ignored table tests.
- Hit-testing units: `panelAt` for each panel rect + edges (border cells
  count as the panel) + header/footer misses + narrow terminal;
  `panelRowAt` on an unscrolled and a SCROLLED panel (windowStart
  consistency: clicking the first visible row selects the right backing
  row) + border/label/padding misses; `windowStart` extraction keeps
  `windowRows` behavior (existing tests stay green).
- Normal mode: click focuses + selects + records `lastLeftPanel`;
  label-click focuses without selecting; wheel moves the hovered
  (unfocused) panel's selection by the configured step without moving
  focus; wheel respects a repo-config `wheel_step` override; clicks/wheel
  ignored under the modal and each popup; click outside panels no-ops.
- Files view: tree click sets `filesTreeFocused` + cursor; commits click
  clears it + moves selection + fires exactly one reload cmd; clicking the
  already-selected commit dedupes (no cmd); wheel over tree vs over
  commits; help-window-over-files-view wheel priority.

## Not doing (YAGNI)

Drag selection; double-click actions (e.g. enter-equivalent); click-to-
close or click-through on popups; right/middle buttons; hover effects;
configurable click behavior.
