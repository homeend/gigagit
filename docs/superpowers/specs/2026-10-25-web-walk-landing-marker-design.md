# gg web — the walk landing marker

Status: approved scope (the user's report, 2026-10-25: "if I open two agent
tabs and I switch between those agents, I don't know what is open there").
Parity row: docs/web-tui-parity.md, "Walk landing marker". Client only.

## Why

In the TUI every walk puts the Branches cursor on the row of what it shows:
the shown session's `└ ●` sub-row (alt+a / alt+t / alt+A / alt+T, alt+q's
session stops — `selectSessionRow`) or the worktree's branch row (alt+w,
alt+q's plain stop — `selectWorktreeBranch`). With two agents in one
worktree that row is the only thing on screen that says which one you are
in: the header and the `*` only name the worktree. The web sidebar has no
cursor, so the effect was dropped when stage D ported the walks.

## Side effects on screen (TUI captures, 140x36, fx21: two worktrees, fake agents)

| Action | Branches cursor | Tab | Other panels |
|---|---|---|---|
| alt+a / alt+A (bound console, next session) | → the shown session's `└ ●` sub-row; drawn dimmed (the ghost cursor) while the console is bound | Branches shown | the console swaps; header + `*` follow the session's worktree; status `Fake [alpha] in wt-a — agent 1 of 2` |
| alt+a / alt+A, two agents in ONE worktree | alternates between the two sub-rows | Branches | header unchanged — the cursor is the only difference |
| alt+A / alt+a "already bound" / "no running terminal" | unchanged | unchanged | status line only |
| alt+q session stop | → that session's sub-row | Branches | console shown; status `… — 1 of 2` |
| alt+q plain stop | → the worktree's branch row (focused, full-text reveal) | Branches, focused | console hidden; status `wt-a — plain worktree, 2 of 2` |
| alt+w (every landing, first-hit close included) | → the target (or viewed) worktree's branch row | Branches, focused | windows close; header + `*` move; status `wt-b — 3 of 3 worktrees` |
| start agent from a `.` menu | unchanged (stays on the row the menu was opened on) | — | console opens bound in that worktree |
| enter on a Branches session sub-row | already on it; stays (ghost while bound) | — | console opens, header follows |
| console unbound (ctrl+]) | not drawn (no ghost: focus is the Commits column) | — | footer |
| console closed (esc) | stays on the sub-row / branch row it was on, now the real cursor | — | header back to the return point |

Not a walk effect, seen once: a transient `! 1 notice` after the first
agent start (gone when `!` was pressed). No action here.

## Port

The web shows a **landing mark**: one row of `#branches-list` styled
`.landed` (a left accent bar and the selection background).

1. **State, not DOM.** `state.landing` is `{sid}`, `{branch}` or `null`;
   `renderBranches` applies the class from it, so the mark survives every
   repaint (the sessions event, an in-place switch, a refresh).
2. **Branches list only**, as the TUI marks the Branches tab only. The
   Worktrees list's session rows are not marked.
3. **Set by:**
   - a walk's session stop (alt+a / alt+t / alt+A / alt+T, alt+q / alt+Q):
     `{sid}` of the shown session, set when the console opens (it opens at
     once, even if the page switch is held or refused);
   - alt+q's page stop: `{branch}` of the walked worktree. Stage D merged
     the TUI's "your windows" and "plain worktree" stops into one page stop;
     it takes the plain stop's landing;
   - alt+w: `{branch}` of the target once the switch happened (`doReroot`
     answered true); the viewed worktree's branch when the press only closes
     windows, or there is one worktree, or none other is reachable. A
     refused press (an operation running, a non-window layer on top) sets
     nothing;
   - a click, or **Open session** in its menu, on a Branches session
     sub-row: `{sid}` (the TUI cursor is on that row when enter opens it).
4. **Other console opens** (a Worktrees session row, the ctrl+\ list, the
   launch dialog, a review, a session just started): the TUI leaves the
   Branches cursor alone, and so does the web — except that a `{sid}` mark
   naming ANOTHER session is cleared. The web shows the Branches list next
   to the console; a mark left on another session would name the wrong agent.
5. **Kept** across a console close or hide, repaints and in-place switches
   (the TUI cursor stays). **Cleared** by a click on another Branches row
   (the web has no sidebar cursor, so the mark exists only where a landing
   put it). **Replaced** by the next landing. A repository switch reloads
   the page.
6. **No row** (the branch filtered out, a detached worktree, the session
   gone): no mark is shown. The TUI keeps its cursor where it was; in the
   web a mark left there would point at the wrong row.
7. **"Brings the Branches tab forward"**: a landing unfolds a folded
   branches section (as `locateCurrentBranch` does) and scrolls the row
   into view (`block: "nearest"`). A sidebar hidden with `t`, or a maximised
   console, stays as it is; the mark is there when the sidebar shows again
   (the TUI moves the cursor under a maximised console too).
8. **One style.** The TUI draws a dimmed ghost under a bound console, the
   real cursor when Branches has the keyboard, and nothing under an
   unbound one. The web sidebar never holds the keyboard, so the mark is
   always drawn the same way.

Pure functions (walk.js, tested with node): `landingAfterOpen(landing, id,
fromBranches)` and `worktreeBranch(worktrees, path)` (the branch checked out
at path by `pathKey`; "" when detached or unlisted).

## Design differences (go in the parity doc)

- The mark is the walk's landing only, not a cursor: an ordinary click on
  another Branches row clears it (TUI: the cursor moves there).
- Other console opens clear a mark that names a different session (point 4).
- No row → no mark (point 6); one style (point 8).

## Out of scope

A real sidebar cursor (j/k in the sidebar); marking the Worktrees list; the
alt+q ring itself (stage D's ruling stands).
