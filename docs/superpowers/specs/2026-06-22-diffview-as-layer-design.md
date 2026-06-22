# Design: promote `diffView` from a `Model` field to a stack layer

**Status:** Design spec (approved to plan).
**Date:** 2026-06-22.
**Branch:** `feat/diffview-as-layer` (off `main` @ `a969818`).
**Companions:** `2026-06-22-windowing-zorder-root-cause.md`,
`2026-06-22-key-routing-by-context.md`,
`2026-06-22-split-layer-windowing-investigation.md`.

---

## Goal

Make the full-screen side-by-side **diff** a member of the single layer stack
(`m.layers`) instead of a sibling `Model` field (`m.diffView`), so that one
ordered structure — not five hand-coded slots — owns its z-order, keyboard, mouse,
render, and return target. This is **Stage 1a** of the windowing direction: the
diff is the last full-screen, single-focus window still living off the stack.

It is **mechanical and single-focus**: the diff has no internal pane focus (its two
columns scroll in lockstep), so it needs none of the deferred tiling / focus-split
machinery. `filesView`/`stashView` stay fields this stage.

## Why now (the bugs this closes)

Because the diff is off-stack, three subsystems special-case it and a workaround
(`clearLayers`) exists solely to compensate:

- **`esc` from a history/picker-opened diff doesn't return to its opener.**
  `openPickerDiff` (`bookmark_popup.go:436`) sets `m.diffView` then **`clearLayers()`**
  — erasing the history/picker layer the diff was opened *over* — so `esc` has no
  layer to return to and falls through to the base/files view.
- **The diff's `.` menu can reflect the wrong window.** `availableActions` derives
  "what's in front" from `m.diffView == nil` / `m.filesView` fields
  (`action_menu.go:54`, `frontIsFilesView`) instead of the top of the stack.
- **`clearLayers` is a lossy hack.** Its own doc comment says the diff is "the
  render base the stack walks over," which is exactly the off-stack modeling we are
  removing.

The newest full-screen surface, `identityView`, was already built *as a layer*
(`identity_popup.go:62` `pushLayer(&identityView{})`); the rebase/squash editor
(`irebaseEditor`) is a layer; history/blame/hunk-picker are layers. The diff is the
sole holdout.

---

## Current state (verified on `main` @ `a969818`)

- `m.diffView *diffView` field (`model.go:76`); companions `diffTag`/`diffNav`/
  `diffPartial`/`diffLong` (`model.go:77-81`).
- Keyboard rung: `model.go:497` (`if m.diffView != nil { return m.updateDiffViewKey(msg) }`),
  below `topLayer()` at `:491`.
- Mouse rung: `mouse.go:43`, below `topLayer()` at `:35`.
- Render: `layerBase()` special-case (`view.go:136`) makes the diff the **backdrop**
  the stack composites over; guard at `view.go:199`.
- `updateDiffViewKey` is a **`Model` method** returning **`tea.Model`**
  (`diff_view.go:577`), reading `v := m.diffView`. `renderDiffView` is a `Model`
  method (`diff_render.go:152`).
- The `layer` interface (`layer_stack.go`):
  ```go
  type layer interface {
      update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
      render(m Model, below string) string
  }
  ```

---

## Design

### 1. `*diffView` implements `layer`

Add methods on the existing type (it already holds all its own state):

```go
func (v *diffView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)  // ex-updateDiffViewKey body
func (v *diffView) render(m Model, below string) string              // ex-renderDiffView body
```

- `update`: relocate the body of `updateDiffViewKey`; the receiver `v` replaces
  `v := m.diffView`; **return `Model`, not `tea.Model`** (adjust the handful of
  returns). Logic is otherwise unchanged.
- `render`: relocate `renderDiffView`; it currently ignores `below` (full-screen
  surface), consistent with the other surfaces.
- Add `*diffView` to **`isFullScreenLayer`** (`layer_stack.go:22`) — it owns the
  whole screen, so it must fold into a popup's backdrop like its peers.

### 2. Drop the `m.diffView` field; reads become `layerOf[*diffView](m)`

Remove `model.go:76`. The ~15 read-sites switch to the existing generic accessor
`layerOf[*diffView](m)` (returns nil when no diff is on the stack). Keep
`diffTag`/`diffNav`/`diffPartial`/`diffLong` on `Model` (session + async-gate
state; **`diffNav` stays** — it powers Home/End file-stepping).

### 3. Open sites push instead of assigning

- **The seam — `openPickerDiff` (`bookmark_popup.go:436`)**: today `m.diffView = v;
  m.clearLayers()`. Becomes `m = m.pushLayer(v)` (no `clearLayers`). This single
  edit converts **all** picker/compare/bookmark/shelf/history-full diff opens that
  route through it (`bookmark_compare.go:43,60`; `bookmark.go:249`;
  `bookmark_popup.go:427,542`; `shelf_actions.go:87,122`; `history_view.go:312`).
- **The 3 direct installers**: `diff_view.go:380` (status/staged diff,
  `openStatusDiff`), `files_view.go:426` (file-tree `enter`) — each
  `pushLayer(&diffView{…})`; and the `diffMsg` handler (next item).

### 4. `diffMsg` populates the on-stack diff **in place** (not "top", not replace)

`g`/`G`/`h`/`b` pressed inside a still-loading diff push another layer over it, so
when the load lands the diff may not be the top entry. Replace
`model.go:216-221`:

```go
case diffMsg:
    dv := layerOf[*diffView](m)
    if dv == nil || msg.tag != m.diffTag {
        return m, nil // closed, or stale
    }
    *dv = *msg.view      // copy loaded content into the on-stack pointer
    dv.loading = false
    return m, nil
```

`msg.view` is self-contained (each load cmd builds a complete `&diffView{…}`), so a
struct copy is safe and preserves stack identity.

### 5. `esc` and the width/close sites pop instead of nil-ing

- `updateDiffViewKey`'s `esc` (`diff_view.go:603`): `m.diffView = nil; m.diffTag=""`
  → `m = m.popLayer(); m.diffTag = ""`. (When the diff handles `esc` it *is* the top
  layer — dispatch only routes here via `topLayer()` — so `popLayer` is correct.)
- WindowSizeMsg too-narrow close (`model.go:193-196`): currently nils the field. A
  resize can arrive while a popup sits **over** the diff, so the diff may not be
  top — `popLayer` (top-only) is wrong here. This needs a **remove-by-identity**
  helper, `removeLayer(l layer) Model` (filter the matched entry out of
  `entries`), added alongside `popLayer`/`clearLayers`. The relayout (else) branch
  at `:198-211` mutates the on-stack diff found via `layerOf` in place — correct
  whether or not it is top.
- Repo-switch invalidation (`model.go:1769`, `m.diffView = nil`): `removeLayer` the
  diff if present (it may sit under nothing or under a popup at switch time).

### 6. `frontIsFilesView` → derive front from `topLayer()` — its own step

`availableActions` (`action_menu.go:54,255,282`) currently reads `m.diffView`/
`m.filesView` to decide what's in front. With the diff on the stack, the diff being
front is `topLayer()` being a `*diffView`. Reworking this to consult `topLayer()`
is the **one genuinely non-mechanical change** — it is the "menu reflects the front
window" fix. Scope it as a **separate task with its own test** (a diff opened over a
files view must show diff actions, not leaked files/commit rows).

---

## Explicitly **untouched** this stage

- **`filesView` / `filesPreview` / `stashView`** stay `Model` fields (the
  focus-split / right-column cases — later stages).
- **`historyView.diff`** (`history_view.go:34`) — `diffView` the *type* is reused as
  history's embedded right preview pane. We promote the **standalone `m.diffView`
  instance only**; `historyView.diff` remains history's internal field. Do **not**
  touch it.
- **`identity_popup.go:303` `clearLayers`** — identity-internal (clears the settings
  stack before applying an op), unrelated to the diff. Leave it. After this stage,
  `clearLayers` has exactly this one caller; keep the function.

---

## User-visible behavior change (intended)

`esc` from a **picker-opened compare diff** (bookmark↔bookmark, focused↔bookmark,
shelf compares, bookmark/shelf-vs-working-tree) now returns **to the picker popup**
it was launched from, instead of dropping to the base layout. This is the
return-to-parent behavior the `worktree-popup-return-stack` work established and is
the desired outcome — but it is a change, so it gets an explicit test (see below)
rather than riding in silently.

Diffs opened from non-stack contexts (status/staged panel, file-tree `enter`,
Commits) have no opener layer, so `esc` returns to the panels exactly as today.

---

## Testing

**Regression net (run throughout — broad, shallow read-site swap):**
`diff_view_test.go`, `compare_*_test.go`, `bookmark_*_test.go`, `shelf_*_test.go`,
`history_view_test.go`, `files_view*_test.go`, `mouse_test.go`,
`action_menu_*_test.go`. Plus `./test.sh race` before merge.

**New tests:**
1. **`esc`-returns-to-picker:** open a bookmark↔bookmark compare from the `g`
   switcher → diff layer on top → `esc` → the bookmark switcher is revealed (still
   on the stack), selection/filter intact. (Today this would land on base.)
2. **`diffMsg` in-place under an overlay:** open a diff (loading), push a switcher
   over it (`g`), deliver the matching `diffMsg` → the on-stack diff’s content
   populates and `loading` clears, with the switcher still on top.
3. **`.`-menu front derivation:** a diff opened over a files view → `availableActions`
   returns diff actions, not files/commit rows (the `frontIsFilesView` step).
4. **mouse wheel:** wheel over a diff scrolls the diff; with a switcher over the
   diff, wheel goes to the switcher (top layer), never the hidden diff.

## Acceptance criteria

- `enter` on a history commit → full-screen diff; `esc` → back to the **history
  list**, selection intact.
- `esc` from any picker-opened compare diff → back to its **picker**, intact.
- The diff’s `.` menu reflects the diff in every open path (no leaked rows).
- Keyboard, mouse, and render agree on diff z-order in every case (no
  diff-specific rung remains in `model.go`/`mouse.go`/`view.go`).
- `clearLayers` no longer has a diff caller; `layerBase()` no longer special-cases
  the diff; `*diffView` is in `isFullScreenLayer`.
- `./test.sh race` green; CHANGELOG updated.

## Risks

- **Broad but shallow:** ~15 read-sites + ~3 installers. The seam (`openPickerDiff`)
  concentrates most open paths; the test suite covers them. Mitigation: swap
  read-sites mechanically, run the suite after each cluster.
- **`diffMsg` in-place** is the one place to get exactly right (copy-not-replace,
  tag-gated, find-not-assume-top) — covered by new test #2.
- **`frontIsFilesView`** is the only judgment change — isolated to its own task/test.

---

## Task outline (for the plan)

1. `*diffView.update`/`render` methods + add to `isFullScreenLayer` (no behavior
   change yet; field still exists, methods delegate). Green.
2. Add `removeLayer(l layer) Model` helper. Convert `openPickerDiff` to `pushLayer`
   (retire its `clearLayers`); switch read-sites to `layerOf`; drop the field.
   `diffMsg` in-place; `esc` → `popLayer`; width/repo close → `removeLayer`. Remove
   the `layerBase` special-case + the `model.go`/`mouse.go` diff rungs. Green + new
   tests 1, 2, 4.
3. `frontIsFilesView` → `topLayer()` derivation. Green + new test 3.
4. CHANGELOG; correct the z-order doc's contradiction-#1 framing; `./test.sh race`.
