# Session window keys — design

Date: 2026-10-10. Status: IMPLEMENTED on `feat/fast-worktree-switch-2`,
merged to main `d57f3b97` the same day (post-merge fixes: alt+b docks a
ctrl+t-maximised console; alt+f/alt+b yield to a popup above the console).

## Purpose

An agent or terminal session window (the console) has three states and a
small, regular set of keys, the same from anywhere: hidden, shown (docked
in the Commits area) or maximized (fullscreen). The keys walk sessions
across worktrees or within one, toggle the size, and toggle whether keys
go to the session.

## Vocabulary

- **Focused**: the console is the selected panel (blue border; in code
  `m.focus == panelCommits` with a console shown).
- **Bound**: keys are routed to the session (in code `console.focused`).
  Bound implies focused; unfocusing unbinds.
- **Docked**: the console occupies the Commits column. **Maximized**: the
  whole body (`console.maximized`).

## Rules (user rulings 2026-10-10)

1. **Showing a session into a worktree**: maximized when that worktree's
   screen is fullscreen (a diff, history, blame, viewer or a maximized
   session on top, or a ctrl+t pin), docked otherwise. The session shown
   is focused and bound. The size is the arriving worktree's business: a
   fullscreen view left behind in the worktree you came from does not
   maximize it (it waits where it was opened). Already so after the
   window-stacks work.
2. **`alt+a` / `alt+t`**: walk every running agent / terminal of the
   repository in the Branches tab's order (the worktree rows, top to
   bottom under the tab's sort; within a worktree by start time), jumping
   worktrees as needed with rule 1. No session: nothing (the status line
   says so, as today).
3. **`alt+A` / `alt+T`** (shift): the same walk restricted to the viewed
   worktree. No session there: nothing, silently.
4. **`alt+w`**: the next worktree; a shown or maximized session hides on
   the way (the session keeps running) and the worktree's other windows
   show. No per-worktree memory of "was shown": a session shows only
   through the walks or enter on its row, and hides through `alt+w`, esc,
   `ctrl+]`'s second press or close.
5. **`alt+f`** on a focused session (bound or not): toggle docked ↔
   maximized; after the toggle the session is bound and focused.
6. **`alt+b`** on a focused session: toggle bound — bound → unbound (as
   `ctrl+]`, incl. its un-maximize of a ctrl+t-maximized docked console),
   unbound → bound (as enter on it).
7. **`alt+U`** is the emergency unlock (was `alt+A`); same semantics, a
   capital so a stray `alt+u` never fires it.
8. `ctrl+]`, esc, enter on a session row, the `x` close and the quit
   guard are unchanged.

## Not in scope

Per-worktree memory of a shown console; the hosted web page; the CLI.

## Testing

- `alt+U` unlocks; `alt+A` no longer does.
- `alt+A` / `alt+T` walk only the viewed worktree's sessions and wrap;
  with none: no console, no status change.
- `alt+a` from a fullscreen diff in C into an agent in A docks (rule 1);
  into an agent in a worktree whose pile has a fullscreen view maximizes.
- `alt+f`: docked → maximized → docked, bound and focused each time; from
  an unbound focused console it binds.
- `alt+b`: bound → unbound keeps the console shown and focused; unbound →
  bound.

## Documentation

Help rows (`help.go`), footer hints (`footer.go`: `[alt+U] unlock`, and
`[alt+f] max` / `[alt+b] bind` while a console is focused), README key
table, CHANGELOG, `docs/CLAUDE-details.md`, the four i18n bundles.
