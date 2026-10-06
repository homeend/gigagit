# alt+a / alt+t: a cycle with a way back — design

Date: 2026-10-06 · Branch: `feat/alt-a-cycle` · Scope: TUI only (gg web unchanged)

## Goal

alt+a (agents) and alt+t (terminals) cycle the repository's running sessions
from ANY screen — a focused agent, the normal panels, a full-screen view such
as a file diff — and the cycle always passes once through the screen the user
came from, so pressing the key repeatedly is a loop that ends where it began.

## User rulings (2026-10-06)

1. Every session shown by alt+a / alt+t is shown **unfocused**; enter focuses
   it (as today).
2. Each cycle holds exactly one non-agent stop, the **return point**:
   - started from a non-agent screen → that screen;
   - started from an agent (focused or not) → the screen that was shown
     before that agent was shown.
3. The return point decides the size: a full-screen return point (a diff,
   history, blame, file viewer, or a ctrl+t-pinned panel) shows the sessions
   **full-screen**; the normal layout shows them docked in the right column.
4. The return screen is kept exactly as it was (cursor, scroll, folds) —
   parked, never closed.
5. gg takes alt+a / alt+t even inside a FOCUSED console; the program in it no
   longer receives them.

## Behaviour

### The ring

`ring = sessionsByLastUsed(repoSessions, kind)` followed by the return point.

- From the return point (no console shown) → the ring's first session.
- From a shown session that is in the ring at index i → index i+1; past the
  last session → the return point.
- A focused session is the most recently used (focusing Touches), so from a
  focused agent A the first press shows the agent used before A; A comes back
  just after the return point. With one agent: A → return point → A …
- A shown session of the OTHER kind (alt+t while an agent shows) → the ring's
  first session; the return point is kept.
- Unfocused shows never Touch (unchanged), so the order cannot reshuffle
  mid-cycle.

Status line on a session stop: unchanged (`%s in %s — %d of %d by last use
[enter] focus`). On the return-point stop: nothing new (the screen is back).

Examples, agents A (most recent), B, C:

| Start | Presses of alt+a |
|---|---|
| Commits column | A · B · C · Commits · A … (docked, unfocused) |
| File diff view | A · B · C · diff · A … (full-screen, unfocused) |
| Focused A (opened from the Worktrees row) | B · C · Worktrees panels · A · B … |
| Focused A that was reached from a diff | B · C · diff · A … (full-screen) |

### The return point

Captured when a console appears while none is shown (any path: alt+a/alt+t,
enter on a session row, the ctrl+\ switcher, the AI-task console). When a
console REPLACES another console (cycling, the switcher), the return point is
carried over unchanged — that is what makes "the screen before the agent"
survive a whole cycle.

It records what showing a console hides or clears today:

| Field | Today `showConsole` … | Now |
|---|---|---|
| live layer stack (only when its top is a full-screen surface) | n/a (alt+a refused under a layer) | parked off the live stack |
| `fullMaxed` / `fullMax` (ctrl+t pin) | clears it | saved, restored |
| `stashView`, `filesPreview` (right-column owners) | clears them | saved, restored |
| `m.focus` | `rememberLeftFocus` → `lastLeftPanel` | saved, restored |

`full = parked stack non-empty || fullMaxed`. A full return point shows every
console of the cycle maximised.

Returning (the ring's last stop, esc / step-out on an unfocused console, the
session exiting-and-being-removed, a repo switch closing a foreign console):
drop the console, restore the fields above. Parked layers go back BENEATH
whatever is on the live stack now (a popup opened over the agent stays on
top). This replaces `closeConsole`'s "focus = lastLeftPanel".

**A parked view is never lost.** Every path that ends a console without
"returning" (`dropConsole` callers: stash list / file preview taking the right
column, commit-scope change) restores parked layers too; they only happen from
the docked layout, whose return point has none, so this is a guard.

A repo switch (`reRoot` → `settleConsole`) that closes a foreign console
discards the parked layers instead of restoring them — they show the old
repository, and a switch pops such surfaces anyway (`steerRefusal` treats
diff/history/blame/file viewer as poppable).

### Where alt+a / alt+t work

- **Focused console**: intercepted before the program (beside step-out and
  the sessions key in `updateConsoleKey`).
- **Unfocused console** (docked or full-screen): as today.
- **Base panels**: as today (`topLayer() == nil`, not typing).
- **NEW — a full-screen VIEW on top**: diff, history, blame, file viewer —
  exactly the surfaces `steerRefusal` already treats as poppable, and not
  while the file viewer's search is being typed. The interactive editors
  (rebase editor, hunk picker) are NOT origins: they hold an operation's
  pending input. Popups, the action menu, the decision modal and a process
  still keep the key from it (unchanged).
- A full-screen surface pushed OVER a shown console (e.g. the palette opened
  a diff from the full-screen agent), then alt+a: the console first returns
  (its parked layers go beneath the surface), then a new cycle starts with
  the whole stack as its return point.

### A full-screen agent that is NOT focused (new state)

Today `maximized` implies `focused`. New: `maximized && !focused` exists
whenever the return point is full.

| Key | Unfocused full-screen console |
|---|---|
| enter, ctrl+t | focus it, stays full-screen (Touches) |
| esc, step-out key | go to the return point |
| alt+a / alt+t | continue the cycle |
| x / X | as the docked console (remove / kill+remove) |
| focus moves (tab, shift+tab, left, h, ctrl+left/right) | swallowed — the panels are hidden |
| the rest of `consolePassthrough` | passed through, as docked |
| anything else | swallowed |

Focused full-screen console, step-out key:
- return point full → unfocused, STILL full-screen (a second press / esc →
  return point);
- return point docked (maximised by ctrl+t from a docked console) → as today:
  unfocused and docked again.

The "focus left the console's column" guard (mouse click on another panel)
does not apply to a full-screen console (no other panel is visible).

When the session of an unfocused/focused full-screen console exits: as today
(steps out → unfocused; it stays full-screen until the user leaves).

### Rendering

No new painting: `render` with an empty live stack already reaches
`renderInterface`, whose `console.maximized` branch draws the console over the
whole body. Popups pushed while the full-screen agent shows composite over it
as over any base screen.

## Code shape

- `console.go`: `consoleState.ret *consoleReturn` (the record above);
  `captureReturn` / `restoreReturn`; `showConsole` captures when `m.console ==
  nil`, carries `ret` over otherwise, sets `maximized = ret.full`;
  `closeConsole` = `restoreReturn`; `cycleSessions` gains the return stop and
  the other-kind rule; `updateConsoleKey` intercepts alt+a/alt+t when focused
  and handles the unfocused-maximised key table.
- `model.go`: the alt+a/alt+t gate also admits one of those views on top (a small `cycleReachable()` helper beside `paletteReachable`).
- `layer_stack.go`: nothing new beyond reusing the park/restore idea
  (`restoreParkedLayers` puts layers on top; the return needs them beneath —
  a sibling helper).
- `help.go` (alt+a / alt+t rows), footer unchanged, i18n ja/ko/zh/ru for any
  new or changed string.
- `docs/CLAUDE-details.md` (the alt+a paragraph), `CHANGELOG.md`, `README.md`
  if it lists alt+a.

## Testing

Unit (`internal/tui`, `t.Parallel()`, fake sessions as the existing console
tests do):

1. From Commits: A, B, return → console nil, focus/stash/preview restored.
2. From a diff view: agents maximised + unfocused, the diff layer parked
   (same pointer) and back on top at the return stop with its cursor intact.
3. From focused A: first press shows B (unfocused), A is not Touched by the
   walk; the return stop is the screen before A.
4. One agent from focused A: alt+a → return point.
5. Focused console: alt+a never reaches the session (no SendKey).
6. alt+t mid alt+a cycle keeps the return point.
7. Unfocused full-screen key table: enter focuses + stays maximised; esc
   returns; tab swallowed.
8. Step-out from focused full-screen: full return → unfocused maximised;
   ctrl+t-from-docked → docked.
9. ctrl+t pin as the return point: restored on return.
10. Session removed while a full-screen agent shows over a parked diff → the
    diff is back.
11. A popup opened over the full-screen agent stays on top when the console
    returns beneath it.
12. alt+a refused while the file viewer's search is typing, and over the
    rebase editor.

e2e: one `[tui]` golden scenario — open a diff, alt+a (full-screen agent),
alt+a (back to the same diff screen).

## Out of scope

gg web (its ctrl+\ switcher is unchanged); configurable keys; any change to
the ctrl+\ popup.
