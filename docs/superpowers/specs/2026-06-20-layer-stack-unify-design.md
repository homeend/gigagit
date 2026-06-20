# Stage 3 — Unify `overlayStack` + `viewStack` into one `layer` stack

**Status:** approved scope, 2026-06-20
**Predecessors:** overlay-stack simple popups (`1277ce7`), conflict process Stage 1
(`dacb846`), remaining popups Stage 2 (`31210dc`).

## Goal

Fold the two parallel window stacks — `overlayStack` (centered popups) and
`viewStack` (full-screen surfaces) — into **one** push-ordered stack of
`layer`s, collapsing the duplicated interface, stack type, push/pop/top/clear
set, and the adjacent `overlayTop`/`stackTop` routing checks repeated at three
sites. **Behavior is identical**; this is a structural unification.

## Scope

**In:** merge `overlay` + `surface` → `layer`; merge `overlayStack` + `viewStack`
→ `layerStack`; collapse routing; re-point accessors and typed message lookups.

**Out (unchanged):** the four single-slot pointer fields — `modal`, `proc`,
`actionMenu`, `diffView`. Rationale: the merge's whole value is removing the
`overlay`/`surface` *duplication* (they are near-identical twins). The four slots
are each one-of-a-kind singletons with no twin, so folding them removes no
duplication and only relocates a pointer into a slice while forcing every direct
`m.diffView` / `m.actionMenu` access in the async message handlers through a
typed stack lookup — cost, zero behavioral payoff. `modal` + `proc`
additionally have a behavioral reason: they are exclusive locks that paint over
the bare panels and hide everything beneath, so they are not stack members by
nature. Folding `actionMenu` / `diffView` is a clean future cleanup if a feature
ever needs to pile on them.

## The unified interface

`overlay` and `surface` differ only in `render`'s signature. The union:

```go
// layer is a window pushed on the layer stack: a full-screen surface (history,
// blame, rebase/conflict/stage editors) or a centered popup (bookmark/shelf
// switchers, content/help, reword, …). The top of the stack owns the keyboard;
// popping reveals the layer beneath (or the panels), whose state was never torn
// down. render composites onto `below` (the accumulated render of everything
// beneath): a surface ignores `below` and returns its own full screen; a popup
// composites its centered box onto `below`.
type layer interface {
    update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
    render(m Model, below string) string
}
```

Surfaces (`historyView`, `blameView`, `irebaseEditor`, `conflictPicker`,
`stagePicker`) gain the `below` parameter and ignore it. Popups already match.

## The unified stack

One pointer field replaces `stack` + `overlays`:

```go
layers *layerStack   // ordered window pile (surfaces + popups); nil/empty = none
```

`layerStack struct{ entries []layer }` with:

- `topLayer() layer` — top or nil (replaces `stackTop` + `overlayTop`)
- `pushLayer(layer) Model` — replaces `pushSurface` + `pushOverlay`
- `popLayer() Model` — replaces `popSurface` + `popOverlay`
- `clearLayers() Model` — replaces `clearStack` + `clearOverlays` (always called
  together today, one adjacent site → one call)
- `layerOf[T layer](m Model) T` — typed reverse-scan (replaces `overlayOf[T]`;
  also serves the surface typed lookups)
- `bookmarkSwitcher()` / `shelfSwitcher()` — re-pointed at `layers`

## Render: a bottom-up walk

```
base := renderInterface()
if diffView != nil { base = renderDiffView() }   // diffView is the base the stack walks over
for _, l := range layers.entries {               // bottom → top
    base = l.render(m, base)                       // surface replaces; popup composites
}
```

This makes `menuBackground()` redundant — it *becomes* the stack-walk result.
`actionMenu` then composites over that walk result; `proc` / `modal` paint over
`renderInterface()` as today (above the stack, exclusive).

**Invariant proven by grep (must hold):** today overlays always render above
surfaces *regardless of push order* (`overlayTop` checked before `stackTop`). A
single push-ordered stack only matches this if no `pushSurface` ever fires while
a popup is live. Verified: all `pushSurface` sites are inside another surface's
`update` (surface→surface, keyboard owned by the surface, no popup live) or are
`b`/`h` panel handlers gated on `canShowFileDiff()` — and `overlayTop` is checked
*before* those handlers, so an open popup eats the key first. No surface is ever
pushed over a live popup.

## Routing collapse

At all three sites the adjacent pair

```
if o := m.overlayTop(); o != nil { … }
if s := m.stackTop();   s != nil { … }
```

collapses to one

```
if l := m.topLayer(); l != nil { … }
```

The dispatch and render sites already check `overlayTop` immediately before
`stackTop`, in the same order — clean collapse. `mouse.go` has a pre-existing
quirk (actionMenu sits *after* diffView there, *before* overlayTop in dispatch);
that is a slot we are **not** touching — leave it. The merge only unifies the
`overlayTop`/`stackTop` pair, which is adjacent and same-order at all three
sites.

### Typed message lookups

`model.go` resolves async results to their surface by type:
`stackTop().(*historyView)` (lines ~210, ~221), `stackTop().(*blameView)`
(~226). These re-point to `layerOf[*historyView](m)` / `layerOf[*blameView](m)`
so loaded list/diff/blame content still finds its surface even if (in principle)
another layer sits above. The conflict-picker/stage-picker/irebase handlers that
`pushSurface` switch to `pushLayer`.

## ClearLayers and the diff handoff

`openBookmarkDiff` / `openPickerDiff` call `clearOverlays()` then `clearStack()`
back-to-back (the only sites) before opening the full-screen `diffView`. These
become a single `clearLayers()`.

## Testing

- **Regression north star:** `cross_compare_return_test.go` (bookmark→shelf→esc
  returns to bookmark, and the symmetric case) must stay green — the unified
  `popLayer` must preserve the return chain.
- The existing overlay tests (`overlayOf`-based) and surface tests
  (`stackTop`-based) re-point to the unified accessors; behavior assertions
  unchanged.
- Tooltip stays in `base` only when nothing is stacked above (unchanged).
- Full `./test.sh race` (unit + e2e) green before merge.

## Migration shape (informs the plan)

1. Introduce `layer` + `layerStack` + accessors; make `surface` and `overlay`
   alias-compatible (add `below` to surfaces, ignore it) so both stacks can be
   driven by the new type. Keep both stacks compiling.
2. Replace the two stacks with the single `layers` field; re-point every
   `push*/pop*/clear*/top*` call and the typed message lookups; delete
   `overlay_stack.go` + `stack.go` duplication, `menuBackground()`.
3. Collapse the three routing sites to `topLayer()`.
4. Re-point accessors (`bookmarkSwitcher`/`shelfSwitcher`/`layerOf`) and tests.

Each step keeps the build green and tests passing.

## Future (footnote)

Folding `actionMenu` and `diffView` into `layers` (so even those pile uniformly)
is a clean follow-up if a feature ever needs it; not done here.
