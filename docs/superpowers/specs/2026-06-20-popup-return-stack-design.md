# Overlay stack + paste-dest prefill — design

**Date:** 2026-06-20

## Problem

1. When the bookmark (`g`) / shelf (`G`) switcher opens a child popup — **paste**
   (`p`), **restore** (`p`), or the **remove** (`x`) confirm — and that child is
   dismissed or completes, the user lands on the panels, not back on the
   switcher. A popup that opens another popup should, when the child closes,
   return to the parent with its state (filter/selection/mark) intact — the same
   on a successful action.
2. The "Paste bookmarked file to a new path" popup starts with an empty
   destination; it should be prefilled from the bookmark's path with a
   `_RESTORED` marker.

## Approach (chosen): an overlay stack, convergent with `viewStack`

The popup layer today has **no stack** — ~15 `*X` pointer fields, each hand-wired
into three parallel precedence chains (dispatch / render / mouse). The full-screen
layer, by contrast, has `viewStack` (`internal/tui/stack.go`): push B, pop B,
you're back on A with A's state intact. We give overlays the same treatment.

**Scope:** build the overlay stack and migrate the popups this feature touches —
the **bookmark switcher, shelf switcher, bookmark paste, shelf restore**. The
other ~13 popups stay as legacy fields and migrate later. The end goal is to
**unify** the overlay stack and `viewStack` into one compositor stack; this
increment is built so that merge is mechanical.

### The `overlay` interface (mirrors `surface`)

```go
// surface (existing):   render(m) string                       // owns the screen
// overlay (new):        render(m, below string) string         // draws over `below`
type overlay interface {
    update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
    render(m Model, below string) string
}
```

The only difference from `surface` is `render` taking `below` and compositing onto
it (`overlayCenter(below, box)`) instead of owning the screen. At unification,
`surface` adopts the same signature (ignoring `below`) and the two become one
`layer`.

### The stack (a structural copy of `viewStack`)

```go
type overlayStack struct{ entries []overlay }
// m.overlays *overlayStack
// pushOverlay / popOverlay / overlayTop / clearOverlays   ↔   push/pop/stackTop/clearStack
```

### Overlays hold their own state (like surfaces)

The four migrated popup structs implement `overlay`: `updateXPopupKey` becomes
`(p *X) update(m, msg)`, `renderXPopup` becomes `(p *X) render(m, below)`. Their
four `Model` fields are **removed** — the live popup lives on the stack. Code and
tests that reached for `m.bookmarkPopup` use accessors `m.bookmarkSwitcher()` /
`m.shelfSwitcher()` (return the popup if it is on the overlay stack, else nil).

### `below` is `menuBackground()`

`menuBackground()` already returns the render of everything beneath the popup
layer (top surface → diff view → panel interface). The overlay stack renders:

```go
if o := m.overlayTop(); o != nil {
    return o.render(m, m.menuBackground())
}
```

So an overlay opened over a history surface floats over that surface for free, and
`menuBackground`'s logic is what folds into the unified stack later.

### Routing — one check replaces four

In dispatch, render, and mouse, a single `overlayTop()` check replaces the four
migrated popups' individual checks, placed where the "global" switcher checks sat
(above the full-screen `stackTop`, since overlays float over surfaces). Mouse:
the top overlay swallows mouse (matches today's switcher/paste behavior). The
remaining legacy popup checks are untouched.

## Return-to-parent — falls out of the stack

- **Open a child** (paste / restore): `pushOverlay(child)`. The switcher stays on
  the stack **beneath** it (not nil'd).
- **Child esc:** `popOverlay()` → the switcher is revealed, state intact.
- **Child success** (the `WriteFile` op): `popOverlay()` the child, then
  `startOp`. The switcher sits beneath and is revealed immediately; it was never
  torn down, so "return after success" needs **no** reopen flag. The switcher's
  `update` ignores action keys while `m.running` (it stays visible but inert
  during the brief write); `opFinishedMsg` is unchanged and reloads the panels
  behind it.
- **Remove (`x`) confirm:** the decision modal stays legacy and is checked above
  `overlayTop`, so it renders over the switcher. **Cancel** → modal nil → switcher
  revealed automatically (the gap this feature set out to close). **Remove
  success** → the existing reload (`bookmarksLoadedMsg`/shelf loaded) **refreshes
  the switcher overlay in place** (replaces the top overlay's items) so the
  deleted row drops off.

## Cross-cutting fixes the migration forces

- **`openPickerDiff`** (the `m`/`c`/enter compare → full-screen diff handoff)
  currently nils the two switcher fields and `clearStack()`. It must instead
  `clearOverlays()` (+ keep `clearStack()`), or a lingering overlay would hide the
  diff (overlays render above surfaces).
- **The `?` cheat sheet** (legacy `contentPopup`, built last session) is gated on a
  switcher being open. The gate `m.contentPopup != nil && (m.bookmarkPopup != nil
  || m.shelfPopup != nil)` becomes `m.contentPopup != nil && m.overlayTop() !=
  nil`, still checked above `overlayTop` in all three sites. esc on the cheat
  sheet → `contentPopup` nil → switcher revealed (unchanged behavior).

## Out of scope

- The other ~13 popups (modal, content/help, repo, settings, branch, rename,
  reword, commit, worktree, pair-op, stash, stash-action, conflict) — migrate
  later, converging toward unification.
- The `m`/`c`/enter **compare** actions still open a full-screen diff (not a
  popup); esc there returns to the panels, by design.
- Visually stacking multiple overlays at once — only the top renders, matching
  `viewStack`.

## Part 2 — prefill the paste destination

Pure helper `restoredPath(p string) string` (`path` package, `/` separators):

- **Dotfile** — basename starts with `.` (`.gitignore`, `.env.local`): append
  `_RESTORED` → `.gitignore_RESTORED`.
- **Has an extension** — last `.` in the basename at index > 0 (`config.go`):
  insert before it → `config_RESTORED.go`.
- **No extension** (`Makefile`): append → `Makefile_RESTORED`.

The bookmark paste popup opens with `dest = restoredPath(b.Path)` (fully editable;
the mandatory-dest guard is now satisfied by default). Scoped to the **bookmark
paste** popup only — shelf restore keeps its deliberate "not prefilled" behavior.

## Testing

- **Overlay stack** unit tests: `pushOverlay`/`popOverlay`/`overlayTop`/
  `clearOverlays` (mirror the `viewStack` tests).
- **Migration**: the four popups' existing tests move to drive via the stack
  (`m.bookmarkSwitcher()` etc.); open/close/key/render behavior unchanged.
- **Return-to-parent**: paste esc → switcher restored with filter preserved;
  paste success (simulated `opFinishedMsg`) → switcher still present; shelf
  restore esc → switcher restored; remove-cancel → switcher restored;
  remove-success → switcher refreshed without the deleted row.
- **`openPickerDiff`**: a compare launched from the switcher over a pushed surface
  clears the overlay so the diff is visible.
- **Cheat sheet**: `?` over the switcher still opens the sheet and esc returns.
- **Part 2**: `restoredPath` cases (`config.go`→`config_RESTORED.go`;
  `a/b/config.go`→`a/b/config_RESTORED.go`; `Makefile`→`Makefile_RESTORED`;
  `.gitignore`→`.gitignore_RESTORED`; `.env.local`→`.env.local_RESTORED`); paste
  popup opens prefilled.
