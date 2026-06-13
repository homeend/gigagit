# Diff view long-line modes: scroll / wrap / truncate (design)

> Extends Spec B (word-wrap, merged). Turns the binary wrap toggle into a
> three-way long-line mode and adds horizontal scrolling.

**Goal:** `w` cycles the diff view's long-line handling through **scroll →
wrap → truncate**, where **scroll** is a new horizontal pan (`←`/`→`, `0`
resets) and **scroll is the default**.

**Architecture:** A tri-state `longMode` on `diffView` replaces the `wrap bool`.
`scroll`/`truncate` use the existing 1:1 display stream; `wrap` uses the
expanded stream (unchanged). Scroll mode renders each line through a
horizontal window (`hOffset`) over the sanitized `(disp, emph)` representation.
Pure TUI plus one `[ui]` config entry; `textdiff`/`domain`/`cache` untouched.

**Tech stack:** Go 1.26, Bubble Tea, lipgloss. Config via `internal/config`.

---

## Motivation

The diff view now handles long lines two ways — truncate (`…`) or word-wrap
(`w`). Horizontal scrolling is the third option the user wants, and they want
it as the **default**: open a diff and pan long lines with `←`/`→` rather than
having them wrapped or cut. So `w` becomes a 3-way cycle and the binary
`wrap`/`diffWrap` flags become a tri-state mode.

## Scope

In scope:

- `longMode` ∈ {`scroll`, `wrap`, `truncate`}; **`scroll` is the zero value
  (default)**. `w` cycles `scroll → wrap → truncate → scroll`. Session-
  remembered (`Model.diffLong` replaces `Model.diffWrap`); shown in the hint.
- **Scroll mode**: one display row per logical line (like truncate), windowed
  by `hOffset` (display columns). `←`/`→` pan by a configurable step (default
  8), `0` resets to column 0; `‹`/`›` markers show off-screen text on each
  side; lockstep across both panes. `←`/`→`/`0` are no-ops in wrap/truncate.
- New `[ui] hscroll_step` config (default 8), mirroring `[ui] wheel_step`.
- The hint line and `help.go` reflect the mode + pan keys.

Out of scope:

- Per-pane independent horizontal scroll (no pane focus exists; lockstep only).
- Mouse / wheel horizontal scroll (keys only).
- Vertical changes — `↑↓`/`pgup`/`pgdn`/`n`/`p`/`f`/`h` are unchanged.
- Any `textdiff`/`domain`/`cache` change.

## Decisions (locked during brainstorming)

- **Default = scroll.** A freshly opened diff is in scroll mode at `hOffset 0`.
- Cycle order **scroll → wrap → truncate** (`(long+1)%3` with `scroll`=0).
- `←`/`→` pan, `0` resets, **`‹`/`›` edge markers** (approved), lockstep panes.
- Pan **step is configurable** (`[ui] hscroll_step`, default 8).
- **Byte-identity safety net:** scroll mode renders a line that fits the pane
  at `hOffset 0` **identically to today's truncate path** (it delegates to
  `diffCell`). Only genuinely clipped or panned lines use the new windowing, so
  the default flipping to scroll does not churn the existing render goldens.

## Components

### `longMode` (tri-state)

```go
// longMode is how the diff view shows lines wider than a pane. The zero value
// (scroll) is the default: pan horizontally with ←/→. w cycles to wrap then
// truncate then back.
type longMode int

const (
	longScroll   longMode = iota // horizontal pan (default)
	longWrap                     // word-wrap across display rows
	longTruncate                 // cut with a trailing …
)
```

`w`: `v.long = (v.long + 1) % 3` → scroll → wrap → truncate → scroll.

### `diffView` state changes

- Replace `wrap bool` with `long longMode`.
- Add `hOffset int` (horizontal scroll column; scroll mode only; starts 0).
- Add `maxCell int` (widest cell display width across all rows, both sides;
  computed in scroll mode at `relayout`, used to clamp `hOffset`).
- `Model.diffWrap bool` → `Model.diffLong longMode` (session default).

### `relayout` (display stream)

- `long == longWrap` → expand each row to display rows (current wrap behavior).
- `longScroll` / `longTruncate` → 1:1 (`disp` mirrors `lines`, `dispBlocks ==
  blocks`) — exactly the current wrap-off path. So scroll mode is a 1:1 stream:
  vertical navigation (`↑↓`/`n`/`p`) is unchanged and `hOffset` is orthogonal.
- In `longScroll`, `relayout` also computes `v.maxCell` = `max` over all
  `lines` of `max(width(sanitizeLine(Left)), width(sanitizeLine(Right)))`
  (lipgloss width; skips gap sides). This is the horizontal extent for
  clamping. (Cheap: one pass over the bounded diff.)

### Render — `diffPaneLines` branches on `long`

- `longWrap` → `segCell` (current).
- `longTruncate` → `diffCell` from column 0 (current; truncates with `…`) —
  **byte-identical to today**.
- `longScroll` → `scrollCell` (new).

```go
// scrollCell renders one pane's line through a horizontal window starting at
// hOffset display columns. When hOffset==0 and the line fits the pane it
// delegates to diffCell (byte-identical to truncate at rest). Otherwise it
// windows the sanitized (disp, emph) to [hOffset, hOffset+tw] display columns,
// renders via styledRuns (emphasis preserved), and overlays edge markers: ‹ in
// the first column when hOffset>0, › in the last when text extends past the
// window.
func scrollCell(no int, text string, spans []textdiff.Span, hOffset, gut, width int, gap, hot bool, hotStyle lipgloss.Style) string
```

- Gap side (Add's left / Del's right) → the existing `·` filler (no windowing).
- Markers replace a content column (like `…` does today), so the cell stays
  exactly `tw` wide and lockstep alignment holds. The markers use the gutter's
  dim style so they read as chrome.
- `hOffset` is shared by both panes (lockstep). Each line shows its own slice
  at the same column offset; a pane whose line is shorter than `hOffset` shows
  blank (with `‹` since there is content to the left).

### Horizontal pan keys

In `updateDiffViewKey`, only when `v.long == longScroll`:

- `left` → `v.hOffset -= m.hscrollStep()`, then clamp.
- `right` → `v.hOffset += m.hscrollStep()`, then clamp.
- `0` → `v.hOffset = 0`.

Clamp: `v.clampHOffset()` sets `hOffset` into `[0, max(0, maxCell - tw)]`,
where `tw = (v.width-1)/2 - gutterWidth(full) - 1` (the pane text width). Called
after a pan key and after a resize. In wrap/truncate these keys fall through
(no-op), as today.

### `w` handler

```go
case "w":
	ord := v.currentBlockOrdinal()
	v.long = (v.long + 1) % 3
	v.hOffset = 0
	v.relayout(v.width)
	m.diffLong = v.long
	// re-anchor to the current change (the existing f/w pattern)
	if len(v.dispBlocks) > 0 { … v.jumpTo(v.dispBlocks[ord], body) } else { v.offset = 0 }
```

### Config: `[ui] hscroll_step`

Mirror `wheel_step` exactly (the `adding-config-entries` skill):

- `config.UIConfig` gains `HScrollStep int \`toml:"hscroll_step"\`` (`<=0` =
  unset).
- `config.Defaults()` sets `HScrollStep: 8`.
- `overlayUI` copies it when `> 0`.
- `Model.hscrollStep() int` returns `m.cfg.UI.HScrollStep` when `> 0`, else 8
  (pre-load fallback).

### Hint + help

- The hint becomes mode-aware (a small function, not a const): it shows
  `[w] lines:<scroll|wrap|trunc>`, and in scroll mode also `[←→/0] pan`. Kept
  short enough that `[esc] close` survives the width truncation
  (`TestRenderDiffViewPanes` invariant): the `[pgup/pgdn] page` group is folded
  into `[↑↓] scroll` wording to make room.
- `help.go` "Diff view" section: the `w` row becomes "cycle long-line mode
  (scroll / wrap / truncate)"; a new row documents `←/→ 0` pan in scroll mode.
- The diff view draws its own hint, so `TestHelpFooterCoverage` /
  footer-registry are uninvolved.

### Session + creation sites

`Model.diffLong longMode` (zero value `longScroll`). Every `diffView`
construction sets `long: m.diffLong` (the two loaders, the two loading stubs).
`hOffset` starts 0. On open, the loader's `applyDiff` → `rebuild` →
`relayout(width)` lays out the stream; scroll mode computes `maxCell`.

## Data flow — opening a diff (default scroll)

1. Loader builds `diffView{long: m.diffLong (=scroll by default), width: w, …}`.
2. `applyDiff` sets rows, `rebuild()` → `relayout(w)` builds the 1:1 `disp` and
   `maxCell`, jumps to the first change.
3. Render: `diffPaneLines` draws each `dRow` via `scrollCell` at `hOffset 0`.
   Lines that fit render exactly as before; long lines show a `›` and can be
   panned with `←`/`→`.

## Edge cases

- **Line shorter than `hOffset`**: pane shows blank with a `‹` (content exists
  to the left). No crash (slice clamps).
- **`maxCell <= tw`** (nothing overflows): `hOffset` clamps to 0; `←`/`→` are
  effectively no-ops; no markers. Identical to truncate at rest.
- **Wide runes / tabs**: windowing measures display columns (lipgloss width)
  over the already-sanitized runes (tabs expanded), so a tab never straddles
  the separator.
- **Gap side**: `·` filler, never windowed.
- **Resize**: `relayout` recomputes `maxCell`; `clampHOffset` re-clamps.
- **Mode is wrap/truncate**: `hOffset`/`maxCell` unused; `←`/`→`/`0` no-op.

## Testing

- `scrollCell`: fit-at-hOffset0 delegates to `diffCell` (byte-identical);
  windowed slice at `hOffset>0`; `‹` shown when `hOffset>0`; `›` shown when text
  extends past the window; exact `tw` width; emphasis preserved across the
  window; gap side renders filler.
- `relayout` in scroll mode: 1:1 `disp`; `maxCell` = widest cell width.
- `w` cycles scroll→wrap→truncate→scroll and writes `m.diffLong`; resets
  `hOffset`; a new diff inherits `m.diffLong`.
- pan keys: `←`/`→` move `hOffset` by `hscrollStep`, clamped to
  `[0, maxCell-tw]`; `0` resets; no-op in wrap/truncate.
- config: `hscroll_step` overlay (repo wins) + `hscrollStep()` fallback 8.
- **All existing `diff_view_test.go` / `diff_render_test.go` pass unchanged** —
  default scroll at `hOffset 0` on fitting lines is byte-identical, and wrap is
  untouched.

## File structure

- Modify: `internal/tui/diff_view.go` — `longMode`, `diffView` fields
  (`long`/`hOffset`/`maxCell`), `relayout` (mode branch + `maxCell`),
  `clampHOffset`, `w`/`left`/`right`/`0` key cases, loaders set `long`.
- Modify: `internal/tui/diff_render.go` — `diffPaneLines` mode branch,
  `scrollCell`, mode-aware hint function.
- Modify: `internal/tui/model.go` — `Model.diffLong`; `hscrollStep()`;
  `WindowSizeMsg` re-clamp; status loading-stub sets `long`.
- Modify: `internal/tui/files_view.go` — tree loading-stub sets `long`.
- Modify: `internal/tui/help.go` — Diff-view `w` + pan rows.
- Modify: `internal/config/config.go` — `UIConfig.HScrollStep`, default,
  overlay. `internal/config/config_test.go` — overlay test.
- Modify: `internal/tui/*_test.go` — new tests; any helper updated.
- Docs: `CHANGELOG.md` (always); `README.md` (diff keymap: `w` cycles, `←/→/0`
  pan; `[ui] hscroll_step` in the config section if it lists `wheel_step`). No
  `CLAUDE.md`/agentskill change (no arch/CLI change). adding-config-entries
  skill is the reference for the config entry.

## Success criteria

- A diff opens in **scroll** mode by default; long lines pan with `←`/`→`
  (step from `[ui] hscroll_step`, default 8), `0` resets, `‹`/`›` mark
  off-screen text, both panes in lockstep.
- `w` cycles scroll → wrap → truncate → scroll; the mode shows in the hint and
  is remembered for the session.
- Wrap and truncate behave exactly as before; existing diff tests pass
  unchanged.
- `./test.sh race` is green.
