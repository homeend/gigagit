# TUI Surface Migration — Everything on the View Stack

**Date:** 2026-06-14
**Status:** Approved — ready for planning
**Depends on:** the view-stack primitive (`internal/tui/stack.go`) and its first
two consumers (history, blame), all on `main`.

---

## 1. Goal

Finish the layout layer (design decision **D1**, deferred when the stack
shipped): make **every** TUI surface a stack entry and delete the three
hand-maintained, mutually-inconsistent `if`-chains that route render, key, and
mouse. After this, the routing invariant is structural — **input owner == top
of stack**, render walks the stack once — so background ops / MCP (M3) can rely
on it, and adding a future surface is a `pushSurface`, never an edit to three
dispatch chains.

The user chose **full purity**: the base grid itself becomes a surface; no
overlay pointer fields (`m.diffView`, `m.popup`, `m.modal`, …) survive.

This is a **pure refactor** — no surface changes what it does.

## 2. The compositing model

`surface` (today: `render(m) string`, `update(m, msg) (Model, tea.Cmd)`) gains a
compositing kind, plus two optional interfaces:

```go
type compositeKind int

const (
	kindReplace    compositeKind = iota // owns the whole screen
	kindOverlay                         // a box composited over what's beneath; owns input
	kindDecoration                      // render-only; never owns input
)

type surface interface {
	render(m Model) string
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	kind() compositeKind
}

// positioned: where to composite an overlay/decoration box. A surface that does
// not implement it is centered.
type positioned interface {
	at(m Model, boxW, boxH, scrW, scrH int) (x, y int)
}

// mouseHandler: a surface that wants mouse events implements this. One that
// does not SWALLOWS mouse while it owns input (modal, centered popups).
type mouseHandler interface {
	mouse(m Model, msg tea.MouseMsg) (Model, tea.Cmd)
}
```

`kindDecoration` exists for completeness but has **no stack consumer** — the one
decoration (tooltip) lives inside `gridSurface.render` (§5). It is documented so
a future overlay that must never steal input has a home.

### The three single walks (replacing the three `if`-chains)

- **render** — find the topmost `kindReplace` (the *base*); render it; composite
  every surface above it via `overlayAt` (centered, or `at(...)`). `overlayCenter`
  is already `overlayAt` at a computed center, so one path serves both.
- **key** — `m.stackTop().update(m, msg)`. (ctrl+c global-quit handling stays
  where it is per surface, as today.)
- **mouse** — if `m.stackTop()` implements `mouseHandler`, dispatch to it; else
  return unchanged (swallow). Reproduces today's "top surface only" precedence.

## 3. Surface inventory

Each existing struct becomes a `surface`. Unique types implement the interface
directly with **thin forwarding** to the existing `Model` render/update methods
(e.g. `func (p *worktreePopup) render(m Model) string { return m.renderWorktreePopup() }`).

| Today (field + if-branch) | Surface | kind | input / mouse |
|---|---|---|---|
| base grid (`renderInterface` + base key `switch` + panel mouse) | **`gridSurface`** (stack[0], always present) | replace | owns; `mouseHandler` |
| `m.modal` (`*decisionState`) | `*decisionState` | replace (full-screen `renderModal`) | owns; swallows |
| `m.diffView` | `*diffView` (gains `render`/`kind`; `update`=`updateDiffViewKey`) | replace | owns; `mouseHandler` (wheel) |
| `m.popup` (`*worktreePopup`) | `*worktreePopup` | overlay (centered) | owns; swallows |
| `m.repoPopup` | `*repoPopup` | overlay (centered) | owns; swallows |
| `m.settings` | `*settingsPopup` | overlay (centered) | owns; swallows |
| `m.branchPopup` | `*branchPopup` | overlay (centered) | owns; swallows |
| `m.contentPopup` (help) | **`helpWindow`** wrapper (§3.1) | overlay (centered) | owns; `mouseHandler` (wheel) |
| `m.filesView` (commit files) | **`commitFilesView`** wrapper (§3.1) | overlay (left column via `at`) | owns; `mouseHandler` |
| history / blame | `*historyView` / `*blameView` (already surfaces) | replace | owns |

### 3.1 The `contentPopup` dual-role wrinkle

`m.contentPopup` (help window) and `m.filesView` (commit files tree) are **both**
`*contentPopup` — distinguished today only by which field holds them, with
different compositing (centered vs left-column) and different handlers
(`updateContentPopupKey` vs `updateFilesViewKey`). Type alone can't tell them
apart, so each gets a thin wrapper surface:

```go
type helpWindow struct{ *contentPopup }
// render→renderContentPopup, update→updateContentPopupKey, kind→overlay (centered),
// mouse→contentPopup wheel scroll.

type commitFilesView struct {
	*contentPopup
	hash, title string // were m.filesHash / m.filesTitle
}
// render→renderFilesView, update→updateFilesViewKey, kind→overlay,
// at→left-column origin, mouse→mouseInFilesView.
```

`surfaceOf[helpWindow]` / `surfaceOf[commitFilesView]` then disambiguate cleanly.

## 4. Deleting the named fields

All `m.diffView/m.modal/m.popup/m.repoPopup/m.settings/m.branchPopup/
m.contentPopup/m.pairPopup/m.filesView` (~120 non-test refs across 14 files) are
removed. A generic typed lookup replaces reads:

```go
// surfaceOf returns the topmost stack entry of type T, if any.
func surfaceOf[T surface](m Model) (T, bool)
```

- **Read** `m.diffView != nil` → `surfaceOf[*diffView](m)`.
- **Open** `m.diffView = &diffView{…}` → `m = m.pushSurface(&diffView{…})`.
- **Close** `m.diffView = nil` → `m = m.popSurface()` (pop the specific surface).
- **Async handlers** (`diffMsg`, `commitFilesMsg`, `WindowSizeMsg`,
  `dataLoadedMsg`) find their surface via `surfaceOf` and keep their existing
  **tag-gating** — the tag/hash/title scalars (`diffTag`, `filesHash`,
  `filesTitle`) move onto the owning surface struct (`diffView`,
  `commitFilesView`).

## 5. The awkward cases

- **`gridSurface`** wraps today's base render (`renderInterface`), the base key
  `switch` (currently inline in `Update`'s `KeyMsg` arm, including the
  `filterTyping` branch), and panel mouse. It is `kindReplace`, pushed as
  stack[0] at init and re-created in `reRoot`. Signature note: the base switch
  has `return m.reRoot(path)` and `reRoot` returns `(tea.Model, tea.Cmd)`;
  inside `gridSurface.update` (which returns `(Model, tea.Cmd)`) this becomes
  `mm, cmd := m.reRoot(path); return mm.(Model), cmd`.
- **tooltip + mark** stay *inside* `gridSurface.render` — they annotate the
  grid's focused row, so they belong to the grid, not the stack ("nothing
  outside the stack" holds). Tooltip suppression becomes structural: draw it
  only when the grid is the top surface (`len(stack)==1`), which is exactly
  today's "no popup/overlay up" rule.
- **filesView** (`commitFilesView`) is an overlay positioned at the left column
  (`at` → left-column origin from `layout()` geometry). The grid renders the
  full 3+1 layout beneath; the files box covers the left column; the Commits
  panel shows through on the right, and `commitFilesView.update` still drives
  commit movement (mutating `m.sel[panelCommits]`, which the grid beneath
  renders). The narrow-width auto-close (`WindowSizeMsg` < 40) becomes "pop the
  `commitFilesView` if present."
- **modal** is `kindReplace` (keeps `renderModal`'s full-screen draw — *not*
  converted to a centered box, out of scope). Top-of-stack with no
  `mouseHandler` reproduces "modal swallows everything."

## 6. Sequencing — staged, always-green

This touches the highest-collision files in the repo. Each step keeps
`./test.sh` (and `race` before merge) green. **The grid converts last** — it is
the trickiest piece (the large base key `switch`, tooltip, mark), so everything
else is already proven on the stack before we touch it, and the established
"stack rides above the base interface" pattern (history/blame already do this)
carries the transition with no hybrid compositing.

1. **Machinery only.** Add `kind()` to the interface (history/blame, the sole
   stack surfaces today, return `replace`); add `positioned`/`mouseHandler`; and
   **generalize the existing stack dispatch into the three walk helpers** so the
   walk can composite `overlay` surfaces over the base, not just full-replace.
   Add `surfaceOf[T]`. The base interface stays the legacy floor (`stack has no
   replace ⇒ renderInterface`). No surface converts yet — pure addition.
2. **Convert each overlay/replace surface onto the stack, above the legacy
   base**, one at a time — **modal → worktree popup → repo popup → settings
   popup → branch popup → help window → pair-op popup → diffView → filesView** —
   deleting the field + its three `if`-branches and retargeting refs via
   `surfaceOf` after each. Replace surfaces (modal, diff) are found as the top
   replace and cover the floor; overlay surfaces composite over it. Suite green
   after each.
3. **Convert the base grid last** into `gridSurface` (stack[0]); remove the
   "stack empty ⇒ base" special-case so the floor is itself a surface; fold
   tooltip + mark (and tooltip's top-only suppression) into `gridSurface.render`.
   Now nothing lives outside the stack.
4. **Cleanup.** A guard test asserting no overlay pointer fields remain on
   `Model` (grep-style or reflect over the struct); re-assert tooltip
   suppression, modal precedence, and input-owner==top.

**Testing.** The existing TUI suite is the safety net — it already pins popup
precedence, tooltip suppression, filesView focus/mouse, diff/history/blame, and
mouse routing; conversions must not perturb it. New focused tests: render walk
finds the base (lowest replace) and composites above it; overlay placement
(popup centered, `commitFilesView` at the left column); tooltip shows only when
the grid is top; `handleMouse` dispatches to the top `mouseHandler` and swallows
otherwise; input owner == `stackTop()`.

## 7. Non-goals
- No new surfaces; **no behavior change** to any popup/diff/blame/grid.
- Modal keeps its full-screen render (no centered-box conversion).
- No MCP/background-op wiring — this only makes the dispatch *ready* for
  "input-owner == top surface."
- No change to `tea.Cmd`/message flows beyond the field→stack mechanics.

## 8. Timing caveat
This edits `model.go`, `view.go`, `mouse.go`, and every popup file — the maximum
merge-collision surface in the repo, amid heavy parallel TUI churn. Like the
roadmap's P4 high-blast items, it wants a **quiet window**, or each staged step
will fight conflicts. Recommend scheduling it when no other TUI feature is
in-flight.

## 9. File map (surfaces own their own files)

| File | Change |
|---|---|
| `internal/tui/stack.go` | `compositeKind` + `kind()` on the interface; `positioned`, `mouseHandler`; the render/key/mouse walk helpers; `surfaceOf[T]` |
| `internal/tui/grid_surface.go` (new) | `gridSurface` (render=`renderInterface`+tooltip+mark, update=base key switch, mouse=panel mouse) |
| `internal/tui/view.go` | `render()` becomes the single stack walk; `renderInterface` moves under `gridSurface` |
| `internal/tui/model.go` | delete overlay fields; `Update` `KeyMsg` arm → `stackTop().update`; base switch → `gridSurface.update`; async handlers use `surfaceOf` |
| `internal/tui/mouse.go` | `handleMouse` → single `mouseHandler` dispatch |
| `internal/tui/diff_view.go`, `worktree_popup.go`, `repo_popup.go`, `settings_popup.go`, `branch_popup.go`, `content_popup.go`, `pairop_popup.go`, `files_view.go` | each gains its `surface` methods (`kind`, forwarding `render`/`update`, `at`/`mouse` where relevant); `content_popup.go` adds the `helpWindow` + `commitFilesView` wrappers |
| `internal/tui/*_test.go` | new dispatch/compositing tests; existing tests adjusted only where they poke deleted fields directly |
