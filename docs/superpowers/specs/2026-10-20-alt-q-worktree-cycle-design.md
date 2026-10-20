# alt+q / alt+Q: cycle one worktree's screens — design

Date: 2026-10-20 · Branch: `feat/alt-q-worktree-cycle` · Scope: TUI only (gg web unchanged)

## Why

alt+a walks every worktree's agents, alt+t every worktree's terminals, alt+w
the worktrees. Nothing walks *one* worktree's screens. Working on one
worktree, the user moves between its agents, its terminals, whatever views
they had open (a diff, a history) and the plain Branches panel for it. They
want one key that walks all of those and comes back around, without losing
the views they had open.

## User rulings (2026-10-20)

1. **alt+q** walks forward and **alt+Q** walks back, both wrapping.
2. The stops for worktree W, in this order:
   1. **your windows**: the screen you were on when the walk began, with
      everything open (diff, history, blame, file viewer, split, files view,
      stash list, which panel had the keyboard). It is *remembered*: it is
      never closed while the walk goes on.
   2. **W's agents**, oldest first.
   3. **W's terminals**, oldest first.
   4. **plain worktree**: W's panels with no window open, the Branches tab
      with the keyboard and its cursor on W's row. This is what alt+w's
      first press shows. It is always the LAST stop before "your windows".
3. Which worktree W is:
   - A console of the viewed worktree is shown: that worktree. The next
     press moves to the stop after that session.
   - The Branches tab has the keyboard and its cursor is on a row that names
     a worktree: that worktree. The walk starts at W's first agent (first
     terminal when it has no agent).
   - The Branches tab has the keyboard and its cursor is on a branch with no
     worktree: alt+q does nothing at all.
   - Any other screen (another panel has the keyboard, a diff is open…):
     the worktree the panels show (`m.viewed`). The current screen is the
     "your windows" stop, so the first press goes to W's first session.
4. W has no running sessions: alt+q toggles between "your windows" and
   "plain worktree".
5. A session is shown over the windows exactly as alt+a shows it today:
   full-screen over a diff, history, blame or viewer, docked otherwise. The
   windows come back on their own stop.
6. The remembered windows are lost when the user opens a window (or closes
   one) while they are put aside, for example opening a diff from the
   "plain worktree" stop. What is on screen then becomes the new "your
   windows" for the next alt+q.

## Behaviour

### The ring

```
ring(W) = [your windows]? ++ agents(W) ++ terminals(W) ++ [plain worktree]
```

- `agents(W)` = `sessionRingIn(false, W)`, `terminals(W)` =
  `sessionRingIn(true, W)`: running sessions only, oldest first. Exited
  sessions are skipped, as in alt+a.
- "your windows" is a stop only when there is something to come back to: at
  least one window (a layer, the files view, the stash list) or a console
  return point that differs from the plain worktree. **Builder's call
  (mine):** a walk started from the plain panels with nothing open has no
  "your windows" stop; it would look the same as "plain worktree".
- A ring with a single stop (no windows, no sessions) does nothing but a
  status line: "no running agent or terminal in this worktree".

### Each stop

- **Session stop:** shown and bound, as alt+a binds the shown session
  (`showConsoleBy(id, true, true)`, `focused = true`). The Branches tab
  selects the session's row (`selectSessionRow`), as alt+a does. Status
  line: "<title> in <worktree> — <i> of <n>" (counting every ring stop).
- **Plain worktree stop:** the console hides (the session keeps running),
  the windows are PUT ASIDE (below), the Branches tab gets the keyboard
  with its cursor on W's row.
- **Your windows stop:** the windows put aside come back exactly as they
  were (cursor, scroll, folds, a history still streaming), with the focus
  and the ctrl+t pin they had.

### Where the windows wait (builder's call, mine)

The windows are put aside ONLY at the plain stop. Every other stop shows
exactly what alt+a shows today:

- **A session stop** is `showConsoleBy` over the live windows: a
  full-screen view on top makes the console full-screen and waits under it
  (`captureReturn` / `consoleParked`), a files view stays live beside a
  docked console, the pin and the focus go into the console's return point.
  A console replacing a console keeps that return point.
- **Leaving the plain stop**, in either direction, first puts the windows
  back live, then takes the next step as if from "your windows". So alt+q
  and alt+Q give the same screen for the same stop, and esc from a session
  stop always returns to the windows (the console's ordinary return).
- **Reaching "your windows" from a session stop** hides the console in
  place (its return point aimed at W, then `closeConsole`): the console's
  ordinary return puts back the windows, the pin and the focus.

### Starting from a shown console

**User ruling (2026-10-20): alt+q walks the worktree selected NOW, and
nothing else.** alt+a / alt+t / alt+w switch the worktree; after such a
switch alt+q walks the new worktree's entries.

- A console of W shown while the panels show W (the normal case, alt+a
  swaps the panels to the console's worktree): the walk is W's. If alt+a
  showed it over a diff in V, V's diff is not a stop: hiding the console
  aims its return point at W, so the windows that come back are W's own
  (V's diff waits in V's slot and comes back whenever V is shown again).
- A docked console of another worktree while the panels show V (the user
  switched the panels under it): W is V, the worktree selected now. The
  console is not one of V's stops; the first press hides it in place and
  walks on from V's windows.

### Putting the windows aside at the plain stop

The plain stop must NOT close anything. `closeWindows()` is the wrong tool:
it stops a history walk, retires a split, sends an open file to the
background. The plain stop *parks* the worktree's whole window group, as a
slot swap does:

- `m.windowState` (the pile, the files view and its whole cluster of
  fields, the stash list, the preview, the window generations) moves into
  a **cycle park** with the focus, the active left tab and the ctrl+t pin
  (process-wide on `Model`); the live group becomes `fresh()` (what a
  never-visited worktree shows) holding a pointer to the park;
- "your windows" assigns the parked group back and restores focus, tab and
  pin.

The park lives on the live `windowState` (a pointer field), so a slot swap
carries it with the worktree (`parkView` / `loadView`); a fresh group or a
recycled slot has none.

Late results: a window-addressed message (`slotMsg`) stamped for the
viewed worktree while the park holds the windows and no popup is on
screen waits on the park's queue, as a sleeping slot's does
(`routeSlotMsg`), and is replayed when the windows come back
(`m.replay`). A history walk writes through its view's pointer and keeps
streaming; `windowState.holds` must see the park so the sweep does not stop
it. A steer request parked in the group (`pendingSteer` / `pendingHint`) is
answered as failed when the windows are put aside
(`failParkedSteer`).

### When the remembered windows are lost

Ruling 6, precisely:

- "A window" means a diff, history, blame, file viewer, split, the files
  view or the stash list. **Popups are not windows** (the palette, the `.`
  menu, help, a confirm): opening one never ends the walk, and alt+q is
  ignored while one is on screen (`altCycleBlocked`).
- While the windows are parked (the plain stop), the user opening a
  window ends the walk. The parked set is closed
  properly, each window as its esc would close it (`closeWindows`-style:
  history walks stopped, splits retired, an open file to the background —
  ctrl+\ still lists it). Nothing is silently dropped.
- Keys that open no window do not end the walk: moving the Branches cursor,
  typing into a bound session, alt+f / alt+b on the console.
- Leaving W without alt+q and without a console (alt+w, a worktree
  switch): the walk ends and the park stays on W's slot
  untouched — it is NOT put live first (a live pile would make alt+w's
  first-hit close it, or alt+a's captureReturn grab it). The next time W is
  shown by anything other than alt+q, the park goes back onto W's pile
  (on the slot's `loadView`), and whatever showed W then applies its
  normal rules. A repo switch drops it as it drops every slot's windows.
- Any console shown while the windows are parked (alt+a / alt+t, an
  agent's task console): the windows are put back live first, then the
  console shows over them exactly as alt+a does (`showConsoleBy` unparks
  before `captureReturn`), so esc from it returns to the windows.
- A walk in progress wins over the cursor rule: at the plain stop, the
  user moving the Branches cursor onto a branch with no worktree and
  pressing alt+q continues the walk (to "your windows"), never a dead key.
  Moving it onto ANOTHER worktree's row and pressing alt+q ends the walk
  (W keeps its park as when leaving W) and starts one for that worktree.

**Builder's call (mine):** detect "a window opened or closed" in the Update
tail (the parked set exists and the live pile / files view / stash list is
no longer empty, or the walk's recorded stop no longer matches the screen),
not per opener.

### Refusals

As alt+w: an input surface on screen (`altCycleBlocked`: a popup, a
decision, text being typed, an editor holding an op's input) → the key is
ignored; an operation running (`!opsIdle`) → a status line, nothing moves.
A refused press changes nothing (no half step).

### Keys

- alt+q / alt+Q reach gg from everywhere alt+a does: the global dispatch in
  `model.go`, the console key router (`console.go`, the alt+a/alt+t/alt+w
  branch), and both passthrough maps (`consolePassthrough`,
  `consoleFullPassthrough`). A bound console's program never receives them.
- Footer hint `[alt+q] this worktree` when the viewed worktree has a
  session or windows to cycle; a help row for alt+q / alt+Q; all text in the
  four bundles.

## Non-goals

- gg web: no change.
- alt+a / alt+t / alt+A / alt+T / alt+w keep their behaviour.
- No config to change the order of stops.

## Anti-goals

- A press that closes or loses windows the user did not ask to lose.
- A press that leaves the keyboard on a surface the user cannot see.
- A walk that gets stuck (a stop that cannot be shown is skipped with a
  status line, never a dead key).

## Left to the builder

- The walk's recorded position (last stop shown, by session id / "plain" /
  "windows") and how it is checked against the screen on the next press.
- Exact status-line wording beyond the rulings (all through `i18n.T`).
- Test layout: unit tests in `internal/tui` (ring order, both directions
  giving the same screen per stop, no-sessions toggle, park/restore keeps a
  history streaming, a popup does not end the walk, loss on a window opened
  at the plain stop, leaving W keeps the park). **Builder's call (mine):**
  no e2e golden — no `tui_*` scenario starts a session today and the
  harness would need a deterministic PTY program first; unit tests cover
  the walk.
- Docs: CHANGELOG, README key table, `docs/CLAUDE-details.md` (the fast
  switch section).
