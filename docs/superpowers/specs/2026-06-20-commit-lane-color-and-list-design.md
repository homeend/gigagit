# Commit lane color + list mode — design

**Phase 4 of the GitKraken-style commit graph effort** (Phase 1 = multi-branch
feed `6c60fda`; Phase 2 = visual lane graph `a31c1f0`; Phase 3a = selected set
`b628545`). Two related capabilities sharing one primitive:

- **Slice 1 — lane-colored commit dots.** Color each commit's `●` node by its
  graph lane, drawn from a small recycled palette (the convention every git
  client follows). Colors the *dot*, not the connector lines (see Out of scope).
- **Slice 2 — list mode.** A `.`-menu toggle that renders the Commits feed as a
  flat list with a single lane-colored `●` gutter and no connector glyphs (the
  GitUp/Tower compact view). Reuses Slice 1's lane→color primitive.

## Why lane-based color (convention)

Every established client — GitKraken, SourceTree, Sublime Merge, gitk, tig,
`git log --graph --color` — colors the **lane** (line of development), not the
branch ref. "Which branch does a commit belong to" is undefined in a DAG (a
commit is reachable from any number of branches; refs name only tips), so the
well-defined thing to color is the lane the commit occupies. Branch *identity*
is already shown separately as the ref pills (`‹*head›‹branch›`, Phase 1). The
engine already returns `Row.Lane` per commit, so no engine change is needed —
**color lives entirely in the TUI; `internal/commitgraph` stays pure/monochrome.**

## The load-bearing constraint: color must not enter the row string

Commit rows are single-sourced: the same string feeds (a) display, (b) the
filter haystack via `panelView`'s `Row(i)` match, and (c) rune-width truncation
in `renderPanel`/`renderWindow`. Injecting lipgloss ANSI escapes *into* the row
string breaks all three — truncation miscounts invisible escape bytes and the
filter would match against color codes.

**Therefore color is applied at render time, after windowing, from separate
metadata — never written into the row string.** A spec-level invariant:
`m.commitRows()` and everything feeding `panelView`/`Row(i)` remain plain text;
no test or code path puts an ANSI escape into a row string.

## Slice 1 — lane-colored dots in graph mode

### Palette (TUI)

```go
// internal/tui/commit_color.go
var lanePalette = []lipgloss.Color{ // ~7 distinct 256-colors, recycled
    "33", "208", "40", "201", "51", "220", "129",
}
func laneColor(lane int) lipgloss.Color { return lanePalette[lane%len(lanePalette)] }
```

Recycling by `lane % len` is the standard approach; two lanes sharing a color
never coexist on screen in practice (a freed lane's color reuse happens lower
down, like GitKraken).

### The render seam — a generic per-row decorator on `winRow`

`renderWindow` already serves three display modes (`dispMode`: cutoff / wrap /
hscroll) and the graph sits on the **left**, exactly where wrap and hscroll cut.
It already applies the per-row `style` **after** slicing each visual line
(`window.go`: `dl[idx].style.Render(padRight(dl[idx].text, w))`, documented as
"styling is applied only after truncation or wrapping … ANSI-safety"). A colored
dot needs *sub-line* styling (one rune a different color), which a single
`lipgloss.Style` can't express — so add a generic (lane-agnostic) decorator
hook to `winRow` that runs in the same post-slice position:

```go
type winRow struct {
    text     string
    prefix   string         // existing: frozen left gutter (winOpts.prefixW)
    style    lipgloss.Style // existing: zero value renders text unchanged
    decorate func(visible string, hscroll, visualLine int) string // NEW, optional
}
```

- `hscroll` — the horizontal offset applied to this row (0 in cutoff/wrap;
  `o.hscroll` in scroll mode). The decorator needs it to relocate the node
  column when the graph has scrolled left.
- `visualLine` — the segment index: 0 for a row's first visual line, 1+ for
  wrap continuation lines (which carry no graph).

`renderWindow` calls `decorate` (when non-nil) per visual segment, in the loop
that builds each `dline`, **after** the `truncate`/`wrapWidth`/`hslice` slice and
**before** `style.Render`. It passes the segment index as `visualLine` and
`o.hscroll`. `renderWindow` itself stays lane-agnostic — the closure (built by
`renderPanel` for the commit panel) carries the node column and palette.

(The existing frozen `prefix`/`prefixW` gutter is an alternative home for the
list-mode dot, but the decorator is one uniform seam covering both the in-text
graph dot and the list gutter, so we use it for both.)

### What each display mode renders (decided, not left open)

The commit panel attaches a decorator that colors exactly **one rune** — the
node `●` — when visible. Its source column in `text` is `len(prefix) + 2*lane`
(`renderPanel` prepends a 2-rune `"  "`/`"> "`/`"◆ "` prefix; the engine places
the node glyph at graph column `2*lane`).

- **cutoff (default):** `hscroll == 0`, graph at the left, never right-truncated;
  color the dot at visible column `nodeCol`. Main case.
- **scroll, hscroll > 0:** if the node column scrolled off the left
  (`nodeCol < hscroll`), nothing to color; otherwise color at the shifted visible
  column `nodeCol - hscroll`.
- **wrap:** only `visualLine == 0` carries graph cells; continuation lines
  (`visualLine >= 1`) have no graph and receive no color.

### Selection interaction

The selected, focused row already gets `style = selectedRow`
(`lipgloss.Reverse(true)`) applied to the whole line. Reverse over a
foreground-colored rune is muddy, so `renderPanel` **omits the decorator for the
selected row** — reverse-video is its highlight. (Per-row choice, no special
logic in `renderWindow`.)

### When the dot is colored

Only when the graph is actually drawn — `commitGraphOn()` (unfiltered + default
sort) — and the row index maps to a `Row` whose lane is known. Otherwise plain
(today's behavior), unchanged.

## Slice 2 — list mode

A presentation toggle orthogonal to the `z` text-overflow modes.

### State + toggle

- `Model.commitListMode bool` (default false → graph mode).
- `.`-menu action `commitViewModeRow()` on the Commits/Branches panel:
  label **Show as list** when in graph mode, **Show as graph** when in list
  mode; `run` flips `commitListMode`. Stable id `commits-viewmode`. No keybind
  (help.go-only advertising, like the scope actions).

### Renderer

In list mode, `commitRows()` produces, per commit, a leading `●` + space +
`hash + refs + subject` (the same plain content, but a single dot gutter instead
of the multi-lane graph prefix). The dot is colored at render time by the **same
decorator seam and `laneColor`**, keyed to `Row.Lane` from the full-feed `Lay`.
The dot's source column is fixed (`len(prefix) + 0`).

### List mode works where the graph cannot

Unlike the connected lane graph (suppressed under filter/sort because hidden
rows break lane topology), list mode draws no connectors, so a per-row
lane-colored dot stays meaningful even when the feed is filtered or re-sorted —
the color still groups commits by their original line of development. So in list
mode the dot shows regardless of filter/sort; only the *selected-row* and
*hscroll-off* rules from Slice 1 gate it.

## Components / files

| File | Responsibility |
|------|----------------|
| `internal/tui/commit_color.go` (new) | `lanePalette`, `laneColor`, the commit node-dot decorator factory. |
| `internal/tui/window.go` | add the `winRow.decorate` field; `renderWindow` calls it per visual segment, post-slice / pre-`style.Render`. |
| `internal/tui/view.go` | `renderPanel` attaches the commit decorator (skips the selected row); `commitRows()` list-mode branch. |
| `internal/tui/commit_scope.go` | `commitViewModeRow()` toggle action. |
| `internal/tui/action_menu.go` | wire `commitViewModeRow` into `availableActions`. |
| `internal/tui/model.go` | `commitListMode bool`. |
| `internal/tui/help.go` | advertise Show as list / Show as graph (no footer). |
| `CHANGELOG.md` | entry. |

## Testing (TDD)

The decorator is pure and unit-testable in isolation; the integration is tested
through `renderWindow`.

1. **Width invariant (the regression guard for the single-sourced-row rule):** a
   colored row windows to the same *visible* width as its plain equivalent —
   `lipgloss.Width(decorated) == lipgloss.Width(plain)`. This proves color added
   no visible columns.
2. **Filter haystack stays plain:** the string returned by `commitRows()` (and
   whatever `panelView`/`Row(i)` matches) contains no ESC byte (`\x1b`).
3. **Mode coverage — not just cutoff:** the decorator colors the node on
   `visualLine 0` in cutoff; colors nothing on a wrap continuation line
   (`visualLine >= 1`); colors nothing when `hscroll > nodeCol` (scrolled off);
   colors at the shifted column `nodeCol - hscroll` when `0 < hscroll <= nodeCol`.
4. **Color present where expected:** in graph mode (unfiltered) a non-selected
   row's decorated output contains the ANSI sequence for `laneColor(lane)`
   around the `●`; the selected row's output does not (reverse instead).
5. **Slice 2:** `commitViewModeRow` flips `commitListMode` and the label toggles
   Show as list ↔ Show as graph; in list mode `commitRows()` yields a `●`-gutter
   row per commit (plain string); the list dot is colored under filter (where the
   graph would be suppressed).

No git/argv change → no real-git or e2e scenario this round (lane data already
has real-git coverage from Phase 2's oracle).

## Out of scope (explicit — green-light from review to pull any in)

- **Full lane-LINE coloring** (color the `│ ╰ ┴ ╮` connectors and vertical bars
  by lane, GitKraken-style). v1 colors the node dot only — the dot is what the
  user asked for and the shared primitive for both slices; coloring connectors
  is where lane-column attribution gets genuinely ambiguous. A clean follow-up:
  the decorator already has the geometry to extend to even-column glyphs later.
- **Per-lane distinct *symbols*** (different glyphs per lane) — non-standard;
  color is the convention.
- **Monochrome symbol fallback** — lipgloss/termenv auto-degrades color on
  no-color terminals (dots become uniform), which is the standard behavior.
- **Lane-count cap / overflow marker** (pre-existing deferral, unrelated).
