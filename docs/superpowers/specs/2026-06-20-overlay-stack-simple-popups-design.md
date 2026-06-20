# Overlay-stack migration — simple popups (Branch 1 of N)

**Date:** 2026-06-20
**Status:** Approved (design)
**Predecessor:** `2026-06-20-popup-return-stack-design.md` (the `overlayStack` was built and 4 popups migrated; merged to `main` as `122475f`).

## Goal

Migrate the 8 *surface-exclusive* legacy popups off their dedicated `Model`
pointer fields and onto the existing `overlayStack` (`internal/tui/overlay_stack.go`),
collapsing each one's three routing checks (dispatch / render / mouse) into the
single `overlayTop()` path the 4 already-migrated popups use.

This is the first of a multi-branch effort. The end state (a later branch) is
to unify `overlayStack` and the full-screen `viewStack` into one compositor
`layer` interface. This branch does **not** touch the unification.

## Background — the established pattern

The `overlay` interface (already in the tree):

```go
type overlay interface {
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	render(m Model, below string) string
}
```

A migrated popup owns its own state (no `Model` field), is opened with
`m.pushOverlay(&x{...})`, closed with `m = m.popOverlay()`, and renders itself
over the layer beneath via `overlayCenter(clipToHeight(below, h), box, w, h)`
with `below = m.menuBackground()`. The dispatch/render/mouse layers route every
overlay through one check:

- **dispatch** (`model.go` `Update`, `tea.KeyMsg` arm): `if o := m.overlayTop(); o != nil { return o.update(m, msg) }` — sits above `stackTop`/`diffView`, below `modal`/`actionMenu`/the `?` cheat-sheet gate.
- **render** (`view.go` `render`): `if o := m.overlayTop(); o != nil { return o.render(m, m.menuBackground()) }`.
- **mouse** (`mouse.go` `handleMouse`): `if m.overlayTop() != nil { return m, nil }` (swallow).

The template to copy is `bookmark_popup.go` (`bookmarkPopup.update`/`render`).

## Scope — the 8 popups

| Popup field | Type | Source file | Opened from | Surface-exclusive | Pops before op |
|---|---|---|---|---|---|
| `popup` | `*worktreePopup` | `worktree_popup.go` | panel keys `W` (model.go:622/628) | yes | yes (nil@221 → `startOp`@222; error path launches no op) |
| `commitPopup` | `*commitPopup` | `commit_popup.go` | panel key `c` (model.go:541) + amend async msg (model.go:978) | yes (Files/Staged only) | yes (nil@104 → op@105) |
| `repoPopup` | `*repoPopup` | `repo_popup.go` | panel key (model.go:811) | yes | yes (nil only; switch via `pendingSwitch`, no `startOp`) |
| `settings` | `*settingsPopup` | `settings_popup.go` | panel key (model.go:818) | yes | yes (nil only) |
| `branchPopup` | `*branchPopup` | `branch_popup.go` | panel keys (model.go:634/648) | yes | yes (nil@47 → op@48) |
| `pairPopup` | `*pairOpPopup` | `pairop_popup.go` | mark/pair flow (mark.go:95) | yes | yes (nil@57 → op@62) |
| `stashPopup` | `*stashPopup` | `stash_popup.go` | panel key (model.go:562) | yes | yes (nil@99 → op@100) |
| `stashAction` | `*stashActionPopup` | `stash_action.go` | `filesView`/`stashView` column handlers (files_view.go:211, stash_view.go:143) | yes | yes (nil → op in each branch) |

**"Surface-exclusive" means** the popup can never be visible while a
full-screen surface (`stackTop`) or the `diffView` is open. This is what makes
the migration behavior-preserving, because migration changes two things at once
for each popup:

1. **Dispatch position** moves from below `stackTop`/`diffView` (lines 406–440)
   to `overlayTop()` (line 391, *above* them).
2. **Render background** moves from `bg = clipToHeight(renderInterface(), h)`
   (view.go:171) to `menuBackground()`.

Both are no-ops *only* when the popup is surface-exclusive: a surface eats keys
before any panel/column handler runs, and an open popup swallows all keys, so
the two are mutually exclusive by construction. When no surface is open,
`menuBackground()` returns exactly `renderInterface()` — the same background.
(`filesView`/`stashView`, the sources of `stashAction`, are column views drawn
*inside* `renderInterface()`, not `stackTop` surfaces, so `menuBackground()`
still draws them underneath.)

**Tooltip:** the legacy path suppresses the panel tooltip whenever any popup is
open (view.go:172 guard) and only adds it on the no-popup branch (172–177). A
migrated overlay returns from `render` at the `overlayTop()` line, *before* the
tooltip logic — so the tooltip stays suppressed under the popup. Behavior
preserved.

**No B1 guard:** every one of the 8 nils its field *before* calling `startOp`,
so the popup is already off the stack during the async op. Unlike the
bookmark/shelf switchers (which stay visible during their write and therefore
need `if m.running { return m, nil }`), none of these 8 needs the guard. This
was verified per-popup (see "Pops before op" column).

## Deferred to Branch 2 (the "special" set)

Not in scope here; each needs individual reasoning:

- **`rewordPopup`** — opened from the `.` action menu (panel-focus-gated, not a
  top-level keypress) and *embeds* a `commitPopup` value
  (`reword_popup.go:39`). Migrate after `commitPopup` so the embedded type is
  settled.
- **`renameBranchPopup`** — also `.`-menu driven, focus-gated.
- **`contentPopup`** — dual-use: the centered help/cheat-sheet popup *and* the
  `filesView` left-column tree share the struct. The `?` cheat-sheet gate
  (`m.contentPopup != nil && m.overlayTop() != nil`) special-cases it.
- **`conflictPopup`** — has the `reopenConflict` async-reload path.
- **`actionMenu`** — higher dispatch precedence than overlays; key-replay.
- **`modal`** — async reply channel; highest precedence; may never become an
  `overlay`.

After Branch 1, the dispatch chain drops from 11 legacy popup checks to 4
(`contentPopup`, `conflictPopup`, `rewordPopup`, `renameBranchPopup`) plus the
already-separate `modal`/`actionMenu`/cheat-sheet checks.

## Per-popup migration recipe (uniform for all 8)

For popup `X` with field `m.X *xType`, current handlers `updateXPopupKey` /
`renderXPopup`:

1. **Implement `overlay` on `*xType`** in its source file:
   ```go
   func (p *xType) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
       // body of updateXPopupKey, with:
       //   m.X            → p
       //   m.X = nil      → m = m.popOverlay()
       //   return (tea.Model, ...) → return (Model, ...)
   }
   func (p *xType) render(m Model, below string) string {
       w, h := m.overlayDims()
       return overlayCenter(clipToHeight(below, h), renderXBox(m, p), w, h)
   }
   ```
   `renderXBox` is the existing `renderXPopup` body (it already returns just the
   modal box; `overlayCenter` was applied by the caller in `view.go`). Keep it a
   plain helper or fold it inline.
2. **Delete** the `X *xType` field from `Model` (`model.go`).
3. **Open-sites:** `m.X = &xType{...}` → `m = m.pushOverlay(&xType{...})`
   (including the cross-file sites: mark.go, files_view.go, stash_view.go, and
   the amend async handler in model.go).
4. **Remove the three legacy routing checks** for `X`:
   - dispatch block in `model.go` `Update`,
   - render block in `view.go` `render`, and remove `m.X` from the
     tooltip-suppression condition (view.go:172),
   - mouse swallow entry in `mouse.go` (the OR-chain at ~line 64).
   The single `overlayTop()` checks already present cover all three.
5. **Test accessor:** add one generic helper (Task 1), then read the live popup
   in production code and tests via `overlayOf[*xType](m)`.

### The generic accessor (added once, Task 1)

```go
// overlayOf returns the topmost overlay of concrete type T on the stack, or
// the zero value (nil for a pointer type) when none is present.
func overlayOf[T overlay](m Model) T {
	var zero T
	if m.overlays == nil {
		return zero
	}
	for i := len(m.overlays.entries) - 1; i >= 0; i-- {
		if p, ok := m.overlays.entries[i].(T); ok {
			return p
		}
	}
	return zero
}
```

Usage: `overlayOf[*commitPopup](m)` (nil when not open). The existing
`bookmarkSwitcher()`/`shelfSwitcher()` may optionally be reimplemented in terms
of it, but that is not required for this branch.

## Test migration

Per the established pattern (from the predecessor branch):

- Field assignments `m.X = newX(...)` → `m = m.pushOverlay(newX(...))`.
- Reads `m.X` → `overlayOf[*xType](m)`.
- Direct handler calls `m.updateXPopupKey(key)` → `m.Update(key)` (routes
  through `overlayTop()` — stronger, end-to-end coverage).

Each popup's existing `_test.go` is migrated alongside its production change, in
the same task, so the suite stays green per commit.

## Verification

- `go build ./cmd/gg` after each popup.
- `./test.sh` per popup task; `./test.sh race` before merge.
- Manual smoke (optional): each popup opens, edits, cancels (esc), and submits
  (op runs) exactly as before; the tooltip stays hidden under each.

## Execution

Subagent-driven, **one popup per task** (last session's stream-idle timeout was
on a multi-popup mega-task). Task 1 is the generic accessor + the first popup;
each subsequent task migrates one popup and its tests. Task review (spec + code
quality) after each, broad whole-branch review at the end, then
`finishing-a-development-branch`.

Suggested task order (commitPopup before anything that would later embed it;
otherwise low-risk first):

1. `overlayOf` helper + `commitPopup`
2. `repoPopup`
3. `settings`
4. `branchPopup`
5. `pairPopup`
6. `stashPopup`
7. `stashAction`
8. `popup` (worktree — the most state, done last)

## Out of scope

- `rewordPopup`, `renameBranchPopup`, `contentPopup`, `conflictPopup`,
  `actionMenu`, `modal` (Branch 2).
- Unifying `overlayStack` + `viewStack` (later branch).
- Any change to popup *behavior*, keybindings, or rendering — this is a pure
  structural migration.
