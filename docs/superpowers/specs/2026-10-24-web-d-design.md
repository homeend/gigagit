# gg web D — agent walks, console scrollback, mouse, keys-to-session

Parity rows "Agent / screen walks" and "Console scrollback + mouse" of
`docs/web-tui-parity.md`. The TUI is the spec (`internal/tui/console*.go`,
`worktree_walk.go`, `help.go` rows alt+a … mouse); a ruling names what a
browser stands in with.

## 1. Walks (alt+a / alt+t / alt+A / alt+T / alt+q / alt+Q)

TUI (`cycleSessionsIn`, `sessionRingIn`, `walkWorktree`):
- The ring: this repository's RUNNING sessions of the kind (agents or
  terminals), ordered by the Worktrees list's row order, then start time,
  then id. alt+A / alt+T: only the viewed worktree's (none: nothing).
- From a bound (focused) console of the ring: the next one, around again.
  Otherwise the first session at or below the viewed worktree, wrapping; a
  shown unbound console that is that one gets bound. The only one, bound:
  `the only running agent session in this repository — already focused`
  (terminal / worktree variants). None: `no running agent session in this
  repository — start one from the . menu of a worktree or a checked-out
  branch` (terminal variant). Success: `<title> in <worktree> — agent i of n`
  (`terminal i of n`).
- alt+q / alt+Q walk the shown worktree's screens: your windows → its agents
  → its terminals → the plain worktree → …; `no running agent or terminal in
  this worktree`; `<title> in <wt> — i of n`, `<wt> — your windows, i of n`.
- The walk keys are gg's even inside a bound console.

Port: the same ring and words (pure `walkRing`, `walkNext`, `screenStops`),
the Worktrees sidebar order standing for the Branches tab's. **Ruling (alt+q
stops):** the web's console lies over the page, which it never destroys, so
"your windows" and "the plain worktree" are one stop: the page (console
closed). The ring is page → agents → terminals.

## 2. alt+f / alt+b

TUI: on a focused console alt+f toggles docked ↔ maximised (bound after
either way); alt+b toggles keys-to-session (bound → unbound as step-out,
unbound → bound as enter). Port: the same on the web console (alt+f =
maximize / un-maximize, alt+b = step out / focus); the foot advertises them.

## 3. Scrollback

TUI: over a console whose program does not take the mouse, the wheel (or
alt+pgup focused, pgup unfocused) enters scroll mode: a frozen view of the
history (`Session.History`); ↑/↓ pgup/pgdn g/G move, space…space then enter
copies lines, a drag copies text, esc or q leaves, any other key leaves and
goes to the agent.

Port: `GET /api/session-history?id=` returns the frozen history as styled
runs (scrollback + screen, `History.Runs`). The console paints it in place
of the live grid (the stream keeps running underneath) with a cursor line;
the keys as the TUI's; space marks the start of a line range and enter
copies the range (or the cursor line); a mouse selection copies on release.

## 4. Mouse

TUI: the mouse goes to the program when it asks (`Input().Mouse`): wheel,
clicks, drags; a full-screen program without mouse support gets ↑/↓ for
the wheel; the wheel works without focusing.

Port: frames carry `mouse` (the program's mode); `POST /api/session-mouse
{id, kind: press|release|motion|wheel, button, x, y, mod}` sends it
(`SendMouse`) when the program asks, else a wheel on the alternate screen
becomes ↑/↓ (`ScrollKey`), else nothing. The page sends cell coordinates
from the grid; a wheel over a program that does not take the mouse on the
main screen enters scroll mode instead.

## Wire

`sessionWire` gains `terminal` (an Open-terminal session). Every new POST:
`writeGuard` + `MaxBytesReader`; a session id resolves through
`domain.Sessions()`.
