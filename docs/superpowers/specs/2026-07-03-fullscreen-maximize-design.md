# T — fullscreen the focused panel

**Date:** 2026-07-03
**Status:** approved

## Problem

`t` maximizes a focused left-column panel to fill the whole left column
(`leftMaxed`/`leftMax`, merged in `da1453f`). There is no way to give a panel
the entire terminal body — the Commits column always keeps its width, and the
Commits panel itself can never take the left column's width.

## Behavior

`T` (shift+t) toggles the focused panel to fill the **entire body** — both
columns, full width and height. It works on:

- any small left-column panel (Files, Staged/Reflog, Branches/Worktrees, Tags,
  Remotes — whatever `leftColumnPanels()` currently shows), and
- the **Commits panel** (hides the left column; the commit graph gets the full
  terminal width).

`t` keeps its current meaning unchanged: left panel → whole left column;
inert on Commits.

### The ladder (left panels)

Three states: normal → column-maximized (`t`) → fullscreen (`T`). Each key
toggles its own level; the `t` pin survives underneath a fullscreen:

| From state       | `t`                          | `T`                                    |
|------------------|------------------------------|----------------------------------------|
| normal           | column-maximize              | fullscreen                             |
| column-maximized | back to normal               | fullscreen (leftMaxed kept underneath) |
| fullscreen       | drop to column-maximize¹     | back to prior state (column or normal) |

¹ `t` while fullscreen clears the fullscreen pin AND sets
`leftMaxed=true, leftMax=focus` — behaviorally "drop one level", never a
hidden double-toggle.

On Commits: `T` toggles fullscreen; `t` stays inert (`canMaximizeLeft` is
unchanged).

### Escape hatches (never trap the user)

While fullscreen, all other panels are hidden and focus cannot leave the
pinned panel. Exits:

- `T` — toggle back to the prior state,
- `t` — (left panels only) drop to column-maximize,
- `esc` — exit fullscreen (back to the prior state, same as `T`). Lowest
  priority: any existing `esc` consumer (layer/popup/modal, an active filter,
  clearing marks, …) wins; fullscreen-exit only claims the key when nothing
  else does. The user always sees why esc did what it did.

Tab switching (`ctrl+←/→`, tab-bar click) stays live while a left panel is
fullscreen and **re-pins fullscreen to the newly shown tab** — the same
re-pin `activateTab` already does for `leftMaxed` (model.go:2251). This also
covers activating a tab while Commits is fullscreen: the pin transfers to
that left panel rather than leaving focus on a hidden box.

## State

Two new fields on `Model`, mirroring the existing pair:

```go
fullMaxed bool  // T has pinned fullMax to fill the entire body
fullMax   panel // valid when fullMaxed
```

`leftMaxed`/`leftMax` are untouched by `T` (that is what makes "T again
returns to column-maximized" fall out for free).

`reRoot` / repo switch: the pin **persists**, matching `leftMaxed` today
(no writer outside the key handler and the `activateTab` re-pin touches it).

## Layout

`layout()` gains one block AFTER the existing left-maximize block (so
fullscreen wins):

```go
if m.fullMaxed && (m.fullMax == panelCommits ||
        slices.Contains(m.leftColumnPanels(), m.fullMax)) {
    // delete every other panel's boxH/pos
    // pinned: pos {0,1}, boxH = bodyH
    // width: g.leftW = w (left panel) or g.rightW = w, g.leftW = 0 (Commits)
}
```

- Same **stale-pin guard** as `t`: if the pinned panel is no longer visible
  (e.g. active tab switched without re-pinning), fall through to the normal
  split rather than blanking the screen.
- Renderers already read `g.leftW`/`g.rightW`, so no per-panel width plumbing.
- The tab header row (row 0) stays when a left panel is fullscreen, rendered
  at full width (`g.leftW = w`) — tab clicks and re-pinning keep working.
  When Commits is fullscreen the left column renders **nothing at all**,
  tab row included (requirement: no degenerate 0-width column artifacts).

## Focus & keys

- `focusOrder()`: when `fullMaxed` (and the pin is valid), return
  `[]panel{m.fullMax}` — tab cycles nowhere, arrows can't land on hidden
  panels.
- `leftReturnTarget()`: when fullscreen on a left panel, return `m.fullMax`;
  when fullscreen on Commits, ← should be a no-op (single-entry focus order
  already gives this).
- Guard: `T` is inert while the files view owns the screen (`filesView !=
  nil`), same as `t`. All popups/modals/layers draw above and are unaffected.
- Mouse: hidden panels have no boxH/pos ⇒ existing hit-testing already skips
  them.

## Advertise

- `footer.go`: add a `T full screen` hint wherever `t maximize` is shown, plus
  on the Commits panel (which today has no `t` hint).
- `help.go`: one new line next to the `t` entry.

## Testing

TDD, in the style of `maximize_left_test.go` (new `maximize_full_test.go`):

1. `T` on a left panel: pinned box is `w`×`bodyH` at {0,1}; every other panel
   (including Commits) has boxH 0.
2. `T` on Commits: Commits box is `w`×`bodyH` at {0,1}; all left panels
   hidden.
3. Ladder transitions: normal→T→T = normal; t→T→T = column-maximized;
   T→t = column-maximized; esc from fullscreen = prior state.
4. `focusOrder()` collapses to the pinned panel; tab does not move focus.
5. Stale pin: fullMax not in the visible set ⇒ normal layout (no blank
   screen).
6. Files view active ⇒ `T` inert.
7. Tab switch while fullscreen re-pins `fullMax` to the newly shown tab
   (both from a left panel and from Commits).
8. Help/footer drift guard picks up the new binding (existing test pattern).

## Out of scope

- Fullscreen for layered surfaces (diff view, files view) — they already own
  the screen.
- Persisting the pin across sessions.
