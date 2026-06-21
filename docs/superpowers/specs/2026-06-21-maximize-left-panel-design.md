# Maximize left-column panel (`t`) — design

## Goal

Let the user enlarge a small left-column TUI panel to fill the entire left
column height, to see more of its content, then restore the normal split — all
on a single key, **`t`**. A per-panel "pin to full column" toggle, sibling to
the existing per-panel `z` display-mode toggle.

## Motivation

The left column packs up to three small stacked boxes (the Branches/Remotes/
Worktrees tab slot, the Files/Tags slot, and Staged), each getting roughly a
third of the body height. A repo with many branches or many changed files
shows only a handful of rows per box. `t` lets the user temporarily devote the
whole left column to whichever box they're working in.

## Behavior

- Focus any **small left-column panel** — the active top tab
  (Branches/Remotes/Worktrees), the active middle tab (Files/Tags), or Staged —
  and press **`t`**. That panel grows to fill the whole left column height (the
  same height the `l` commit-files view takes over); the other two left panels
  are hidden.
- Press **`t`** again to restore the normal split.
- The pin is **sticky** until toggled off — it does not auto-restore when focus
  moves. This matches how `z` (display mode) is sticky per panel.

### Semantics: pin the focused panel

`t` pins *the focused panel*. It is not a "zoom mode that follows focus". The
pinned panel is a specific panel, remembered until `t` toggles it off.

### Navigation while pinned

- `tab` / `←` / `→` move only between the pinned panel and Commits — while
  pinned, the focus order collapses to `[pinnedPanel, panelCommits]`, so it is
  impossible to focus a hidden left panel (no trap).
- `ctrl+←/→` still cycles **within the pinned slot's own tab group** and
  re-pins the newly shown tab:
  - Pinned on the top slot → `ctrl+←/→` cycles Branches ↔ Remotes ↔ Worktrees,
    re-pinning the new tab (it stays full-height).
  - Pinned on the middle slot → `ctrl+←/→` cycles Files ↔ Tags, re-pinning.
  - Pinned on Staged → Staged has no tab group, so `ctrl+←/→` is inert.
- Crossing to a *different* slot (e.g. from a pinned top tab to Files) requires
  toggling `t` off first.

### Scope guards (v1)

- **Left panels only.** `t` on the Commits panel is a no-op. (A symmetric
  "Commits full-width" — hide the left column — is a tempting follow-up; it is
  explicitly out of scope for v1.)
- **No-op on narrow terminals** (`width < 40`): the layout collapses to a
  single Commits column and there is no left column to maximize.
- **Inert while another surface owns the screen.** When the `l` files view
  (`filesView != nil`) or the stash view occupies the left/right area, focus is
  not on a small left panel, so `t` does nothing. (Staged stays a left panel
  while the stash view is open on the right, so `t` on Staged still works there.)

## Implementation

Everything is driven off `layout()` (`internal/tui/viewstate.go`), the single
source of truth for panel geometry shared by rendering, paging
(`panelRowsCap`/`pageStep`), and mouse hit-testing (`panelAt`/`panelRowAt`).
Maximizing therefore means *only* changing a panel's `boxH`/`pos`; every
consumer follows automatically. This is deliberately unlike `renderFilesView`,
which is a separate full-screen surface.

### Model state

Add one field to `Model` (`internal/tui/model.go`):

```go
leftMax panel // the pinned full-column left panel; panelCount = none
```

`panelBranches == 0` is a valid panel, so the zero value cannot mean "none".
Initialize `leftMax: panelCount` in the Model constructor (the sentinel — there
is no panel with that index).

### The logical-left-panel-set invariant (load-bearing)

Maximize works by zeroing the non-pinned left panels' `boxH`. Several existing
sites derive *reachability* and *render membership* from `boxH > 0`, and would
break when a panel is hidden by maximize. The fix: panel reachability and the
rendered left-box set must come from a **logical** left-panel set, never from
the maximize-zeroed `boxH`.

Introduce a helper on `Model`:

```go
// leftColumnPanels returns the left-column panels that exist for the current
// terminal size, independent of any maximize. Staged is present only when the
// terminal is tall enough that the normal split shows it.
func (m Model) leftColumnPanels() []panel
```

It returns `[activeLeftTab, middleTab()]`, appending `panelStaged` when the
terminal is tall enough to show Staged in the *normal* (non-maximized) split
(the `bodyH >= 12` branch in `layout()`). Drive the three fragile sites off it:

- **`layout()`**: compute the normal split first, then, if `leftMax` names a
  panel in `leftColumnPanels()`, overwrite the left column so the pinned panel
  gets `boxH = bodyH` at `pos{0,1}` and the other left panels get no `boxH`/`pos`
  entry (hidden, `boxH == 0`). Commits geometry is unchanged.
- **`focusOrder()`** (model.go:1161): when `leftMax` is active, return
  `[leftMax, panelCommits]`. Otherwise return the existing order, but derive the
  Staged membership from `leftColumnPanels()` rather than `boxH[panelStaged] > 0`
  (so it stays correct even though maximize can't be active here when not pinned
  — keeping the two in one place avoids drift).
- **`leftReturnTarget()`** (model.go:1188): when `leftMax` is active, `←` from
  Commits returns to `leftMax`. Otherwise unchanged.
- **Render assembly** (`renderInterface`, view.go:343–350): today the top
  (`active`) and middle (`mt`) boxes are rendered unconditionally; only Staged
  is guarded by `boxH > 0`. Change it to iterate `leftColumnPanels()` and render
  each, **skipping any with `boxH <= 0`**, so a hidden (maximized-away) panel is
  not drawn as a degenerate zero-height box. The pinned panel renders at
  `boxH == bodyH`.

### Key handler

In the main key dispatch (`model.go`, alongside `case "z"`):

```go
case "t": // toggle maximize of the focused left-column panel
    if m.canMaximizeLeft() {
        if m.leftMax == m.focus {
            m.leftMax = panelCount // restore
        } else {
            m.leftMax = m.focus // pin
        }
    }
    return m, nil
```

`canMaximizeLeft()` reports whether `m.focus` is one of `leftColumnPanels()` and
no full-screen surface (`filesView`/`stashView`-on-the-left) owns it. On a
narrow terminal `leftColumnPanels()` is effectively empty of the focused panel
(the single-column branch focuses Commits), so the guard naturally fails.

### ctrl+←/→ re-pinning

The existing `ctrl+left`/`ctrl+right` handler (model.go:716) switches tabs and
sets focus. Extend it so that **while `leftMax` is active**, after it updates
`activeFilesTab`/`activeLeftTab` and `m.focus`, it sets `m.leftMax = m.focus`
(re-pin the newly shown tab). When pinned on Staged (no tab group), the handler
must not move focus to a hidden top tab — guard so `ctrl+←/→` is a no-op when
`leftMax == panelStaged`.

### Discoverability

- **footer.go**: add a `{"maximize", "t", "[t] max", ...}` entry next to the
  `z` view entry (scope: the left panels / `opsIdle`). Keep the hint short — the
  footer truncates to width.
- **help.go**: add an `r("t", "maximize the focused left panel to fill the left column")`
  line in the relevant panel help sections (mirroring where `z` is listed).

## Testing

Pure / near-pure unit tests (no `startOp`, no svc — avoids the nil-svc panic
class):

- `leftColumnPanels()` returns the right set at tall vs short terminal sizes.
- `layout()` with `leftMax` set: the pinned panel gets `boxH == bodyH` at
  `pos{0,1}`; the other left panels get `boxH == 0`; Commits geometry unchanged.
- `focusOrder()` collapses to `[leftMax, panelCommits]` when pinned; unchanged
  when not.
- `leftReturnTarget()` returns `leftMax` when pinned.
- Key handler: `t` on a focused left panel sets `leftMax`; `t` again clears it;
  `t` on Commits / narrow terminal is a no-op; `t` while `filesView != nil` is a
  no-op.
- `ctrl+←/→` while pinned on the top slot re-pins the new tab (`leftMax` follows
  `activeLeftTab`); while pinned on the middle slot re-pins Files↔Tags; while
  pinned on Staged is a no-op.
- A `renderInterface`/`renderPanel` end-to-end test: with `leftMax` set, the
  rendered left column contains exactly the pinned panel's box (no degenerate
  zero-height box for the hidden panels), and it occupies the full body height.

## Out of scope (v1)

- Commits full-width (hide the left column) — symmetric follow-up, not built.
- Persisting the pin across restarts / in config.
- A CLI surface (this is a TUI-only interaction; no `agentskill` bump).
