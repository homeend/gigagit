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
   - A console is shown: the console's worktree. The next press moves to
     the stop after that session.
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

### One home for the windows (builder's call, mine)

From the first press until the "your windows" stop, every remembered
window lives in ONE place, the cycle park (below), whichever direction the
user walks:

- At a session stop the console shows **full-screen** when the park holds
  a full-screen view (diff, history, blame, viewer, split) or the ctrl+t
  pin was on, **docked** otherwise. alt+q and alt+Q give the same screen
  for the same stop.
- The files view and the stash list are parked too: beside a docked
  console the left column shows W's plain panels.
- The pin (`fullMaxed`/`fullMax`, process-wide on `Model`) and the focus
  are recorded in the park and restored at the "your windows" stop.

### Starting from a shown console

When the first alt+q press finds a console already shown (alt+a showed it,
or the user opened it), its return point and the slot's `consoleParked`
are MOVED into the cycle park; they are "your windows". The console's
return point is then empty. When the walk ends (below) with a console
still shown, the park goes back under that console as its displaced views,
so esc from the console still returns to them.

**User ruling (2026-10-20): alt+q walks the worktree selected NOW, and
nothing else.** alt+a / alt+t / alt+w switch the worktree; after such a
switch alt+q walks the new worktree's entries. So a console alt+a showed
for W over a diff in V is a walk of W only: its stops are W's sessions,
plain W and W's OWN windows. V's diff is not a stop; it waits in V's slot
and comes back whenever V is shown again. Only W's windows are moved into
the cycle park; a return point naming another worktree is dropped from the
console (V keeps its windows on its own slot).

A console of another worktree than the one the panels show (the user
switched panels under a docked console): W is still the console's
worktree; reaching the plain stop shows W's panels (a fast switch, as
alt+a's show does).

### Putting the windows aside

The plain stop must NOT close anything. `closeWindows()` is the wrong tool:
it stops a history walk, retires a split, sends an open file to the
background. The plain stop *parks* instead, as `captureReturn` /
`displaceUnderConsole` park a console's displaced views:

- the whole live pile (`layers.entries`), the files view, the stash list
  and the files preview move into a new `windowState` field, the **cycle
  park**;
- they come back at the "your windows" stop, in order, under nothing.

**Builder's call (mine):** the cycle park is its own field on `windowState`
(it travels with the worktree, as `consoleParked` does), NOT
`consoleParked`. `captureReturn` overwrites `consoleParked`, so an alt+a
from the plain stop would lose the parked windows and leak a history walk.

Late results must keep reaching parked windows (a diff still loading, a
history streaming). `windowState.holds` and `dispatchParkedAware` know only
`consoleParked` today; extend both to the cycle park with the same
put-beneath / park-again dance. `loadView` and `saveView` (slot swaps) must
carry the cycle park untouched.

### When the remembered windows are lost

Ruling 6, precisely:

- "A window" means a diff, history, blame, file viewer, split, the files
  view or the stash list. **Popups are not windows** (the palette, the `.`
  menu, help, a confirm): opening one never ends the walk, and alt+q is
  ignored while one is on screen (`altCycleBlocked`).
- While the windows are parked (any stop other than "your windows"), the
  user opening or closing a window ends the walk. The parked set is closed
  properly, each window as its esc would close it (`closeWindows`-style:
  history walks stopped, splits retired, an open file to the background —
  ctrl+\ still lists it). Nothing is silently dropped.
- Keys that open no window do not end the walk: moving the Branches cursor,
  typing into a bound session, alt+f / alt+b on the console.
- Leaving W without alt+q (alt+w, alt+a / alt+t showing another worktree's
  session, a worktree switch): the walk ends and the park stays on W's slot
  untouched — it is NOT put live first (a live pile would make alt+w's
  first-hit close it, or alt+a's captureReturn grab it). The next time W is
  shown by anything other than alt+q, the park goes back onto W's pile
  (on the slot's `loadView`), and whatever showed W then applies its
  normal rules. A repo switch drops it as it drops every slot's windows.
- alt+a / alt+t to a session IN W from a parked stop: the walk ends and
  the park becomes that console's displaced views (`consoleParked`), so
  esc from the console returns to the windows.
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
