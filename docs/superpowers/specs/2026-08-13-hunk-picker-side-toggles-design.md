# Hunk picker: side toggles, checkbox hierarchy, output pane

**Date:** 2026-08-13
**Status:** Approved design (sub-project 1 of 3: TUI toggles → unstage picker → web parity)
**Surface:** `internal/hunkpick`, `internal/tui/conflict_picker.go` (+ help/i18n/docs)

## Problem

In the hunk picker (conflict resolve `x`→enter, hunk staging `H`) the
whole-region keys `c`/`i` are exclusive: taking one side deselects the
other, so "keep both sides of this region" needs tedious line-by-line
picking, and "everything from the left except one region" needs per-region
work. The `result:` preview is also buried inline under each line-by-line
block. GitKraken's merge tool (the reference UI) shows the better shape:
checkboxes at three levels — whole side, per group, per line — plus a
persistent Output pane with the live merged result.

## Feature

**One unified selection model.** Every selection is an ordered line pick
(the existing `Mode=LineByLine` + `Picks` representation). The three
levels manipulate the same picks:

- **Line toggle** — `space`, as today: pick/unpick the cursor line.
- **Group side toggle** — `c` / `i` on the focused region: if ALL of that
  side's lines are picked, unpick them all; otherwise append the missing
  ones top-to-bottom (checkbox tri-state). Toggling one side never clears
  the other — a region can have left, right, or both sides selected.
- **Side master toggle** — `C` / `I`: the same tri-state across ALL
  regions (if every region has that side fully picked → clear that side
  everywhere; else complete it everywhere).

**Result order:** within a region, picked lines emit in pick order (a side
toggle appends its side's lines in natural top-to-bottom order — so
toggling `i` then `c` puts incoming above current). Regions always emit at
their document position; mass selection cannot scramble the file.

**Decided vs undecided:** an untouched region (`Mode == Undecided`) blocks
`enter` in the conflict flavor, as today. Any toggle marks the region
touched (`Mode = LineByLine`); a touched region with zero picks is
**decided empty** — a deliberate "drop both sides" resolution that `enter`
accepts.

**Checkbox hierarchy (rendering):**

- **Column-label row** gains a master checkbox per side:
  `[x] current ║ [ ] incoming`, `[~]` when only some regions have that
  side fully picked. Reflects/driven by `C`/`I`.
- **Group header row** changes from full-width to a paired two-column row:
  left cell `▶ [x] current · region 1/2`, right cell
  `[ ] incoming · <state>`. The per-side checkbox is `[x]` all picked /
  `[~]` some / `[ ]` none. `<state>` is a compact suffix for what the
  checkboxes can't show: `undecided` (untouched), `none` (decided empty),
  `current first` / `incoming first` (both sides on — the order hint).
  Plain single-side states get no suffix.
- **Line rows** show the `[ ]`/`[x]` gutter checkbox on every selectable
  line in every region (today it appears only in line-by-line mode) —
  everything is picks now.

**Output pane:** the inline `result:` lines are removed. A bottom pane
(≈⅓ of the picker body, minimum 3 lines, separated by a rule titled
`output`) shows the assembled result live: literals + each region's picked
lines; an undecided region contributes one dim placeholder line
(`‹region N undecided›`). The pane scrolls to keep the focused region's
contribution in view as the cursor moves. `o` collapses/expands the pane
(default: expanded). The grid's display mode (`z`, `shift+←/→`) applies to
the pane's lines the same way; `alt+↑/↓` free-scroll keeps applying to the
grid only.

**Both flavors** (conflict resolve and `H` staging) get identical
behavior — the surface is shared; labels stay injected
(current/incoming vs index/working).

## Design

### `internal/hunkpick`

New helpers; existing API (`Mode`, `TakeCurrent`, `TakeIncoming`,
`SetAll`, `ToggleLine`, `Resolved`, `Pending`) unchanged — the web
frontend keeps compiling and behaving as today until sub-project 3.

- `func (b *Block) EnsurePicks()` — materialize legacy whole-side modes
  into picks: `TakeCurrent` → picks = all current lines in order (same for
  `TakeIncoming`), then `Mode = LineByLine`. `Undecided` → just
  `Mode = LineByLine` (empty picks). No-op when already `LineByLine`.
- `func (b *Block) SideState(s Side) (all, any bool)` — whether all/any of
  side `s`'s lines are picked (interprets legacy modes: `TakeCurrent`
  counts as all-current/none-incoming, etc.). A zero-line side is always
  `all=false, any=false`: an empty side can never be "fully picked", its
  checkbox renders `[ ]`, and toggling it is a no-op.
- `func (b *Block) ToggleSide(s Side)` — `EnsurePicks`, then tri-state: if
  every line of `s` is picked → remove all picks of side `s` (other side's
  picks keep their order); else append the missing lines of `s` in
  top-to-bottom order.
- `func (d *Doc) ToggleSideAll(s Side)` — tri-state over blocks with at
  least one line on side `s`: if every such block has side `s` fully
  picked → remove side `s`'s picks from those blocks (they were
  necessarily touched already; blocks without `s`-lines are left alone);
  else `EnsurePicks` + complete side `s` on every block that has
  `s`-lines. Note the consequence, deliberate per the approved design:
  completing then clearing a side leaves those regions touched-and-empty,
  i.e. decided empty.
- `func (d *Doc) SideStateAll(s Side) (all, any bool)` — aggregate for the
  master checkbox: `all` = every block with `s`-lines has `s` fully
  picked; `any` = at least one `s` pick anywhere.
- `func (b *Block) ResolvedLines() ([]string, bool)` — exported wrapper
  over the private `resolved`, for the TUI's output pane (per-block lines,
  `ok=false` when undecided).

Decided-empty already works: `LineByLine` with zero picks resolves to
nothing with `ok=true`, and `Pending` counts only `Undecided`.

### `internal/tui/conflict_picker.go`

- Keys: `c`/`i` → `b.ToggleSide(side)` on the focused block; `C`/`I` →
  `e.doc.ToggleSideAll(side)`; `space` → `b.EnsurePicks()` then
  `b.ToggleLine(e.side, e.line)` (replacing today's `Picks = nil` reset).
  `o` toggles the output pane. Everything else (nav, alt-scroll,
  snap-back, enter gate, esc) unchanged.
- Rendering:
  - `columnLabels` prefixes each label with the master checkbox from
    `SideStateAll` (`[x]`/`[~]`/`[ ]`).
  - The group header becomes a paired `colRow` (left/right cells as
    specified above); `badge()` is replaced by the checkbox + suffix
    derivation (`SideState` per side; suffix from undecided / empty /
    both-order). Both-order = which side owns the first pick.
  - `pickerCell` renders the tick for every line regardless of mode
    (`[x]` iff picked).
  - Body layout: grid gets the remaining height after header, column
    labels, output pane (when expanded: `max(3, bodyH/3)` lines + 1 rule
    line), blank, hints. The output pane is a plain full-width slice
    renderer over the assembled lines (hslice/wrap/truncate per the active
    display mode), windowed with `windowStart` around its own anchor = the
    line where the focused region's contribution starts.
- Hints: `[c] current` / `[i] incoming` stay (now toggles), add
  `[o] output`. Help window rows updated to toggle wording; new rows for
  `o` and the checkbox levels.

### i18n

New/changed keys (all four bundles): the group-header cell formats, the
state suffixes (`undecided`, `none`, `current first`, `incoming first`),
`[o] output` hint, `output` rule title, the placeholder
`‹region %d undecided›`, updated help texts (`c/i` toggle wording, `C/I`
master wording, `o` row), README rows for `H` and `x`. Obsolete keys
(the old `take the whole region…` help texts, `result:`) are removed from
code and bundles.

## Out of scope

- The unstage picker (HEAD↔index deselection) — sub-project 2.
- Web parity (checkbox/output UI in the browser conflict picker) —
  sub-project 3, on `web-dev`. Web's use of `TakeCurrent`/`SetAll` is
  untouched here.
- Mouse interaction with checkboxes; `b`-style extra keys; persistence of
  the `o` collapse across sessions.

## Testing

`internal/hunkpick`: EnsurePicks materialization from each mode;
ToggleSide tri-state (complete-partial, clear-full, no-op on empty side,
preserves other side's order); ToggleSideAll aggregate tri-state;
SideState/SideStateAll including legacy modes and empty sides; both-order
result assembly (i-then-c vs c-then-i); decided-empty resolution.

`internal/tui`: key flows (c toggles on then off; c then i = both with
order; C partial → complete → clear; space materializes instead of
clearing an existing side pick); enter gate (untouched blocks vs
decided-empty allows apply); render assertions for master checkboxes,
paired group header with suffixes, always-on line ticks, output pane
content + placeholder + `o` collapse + focus-follow; i18n AST gates.
