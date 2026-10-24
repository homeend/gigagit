# gg web — a console shows in its own worktree; alt+w

User report: alt+a / alt+t to an agent of another worktree switch the
worktree in the TUI; in the web the page stays where it was. And alt+w (the
TUI's worktree cycle) is missing.

## The TUI (the spec)

- `showConsoleBy` (`internal/tui/console.go`): EVERY console show — a walk,
  a session row, an agent just started — switches the viewed worktree to
  the session's when it is a worktree of this repository. A refusal (an op
  running: "an operation is running — switch once it has finished") still
  shows the console; the swap waits.
- A console replacing a console keeps the way back (`ret`): the worktree
  the first console was shown over.
- `closeConsole` → `returnView`: the console's close brings that worktree
  back; a gone one says "<name> is gone — showing <viewed>".
- `cycleWorktrees` (alt+w, `worktree_view.go`): ignored over an input
  surface; refused while an op runs; when anything covers the worktree's
  panels (a console, a diff, history, blame, a file viewer) the first press
  closes them (a console is hidden — no return) and says
  "<name> — alt+w again for the next worktree" / "console hidden — alt+w
  again for the next worktree" / the one-worktree forms; otherwise the next
  worktree of the Worktrees list's ring, "<name> — <i> of <n> worktrees".

## The web

A web worktree switch is `/api/reroot` + a page reload (`doReroot`). So:

- `openConsole` of a session in another LISTED worktree (the Worktrees
  list's rows) reroots there, carrying `{console, ret, line, max}` through
  the reload (`switchCarry`, a JSON `sessionStorage` key beside
  `gg-open-tour`); boot reopens the console focused, puts the walk's line
  back and keeps `ret`.
- **Ruling (op running):** the console opens in place and the line says
  "an operation is running — switch once it has finished"; no queue — the
  TUI's waiting swap is not ported (cost: press the key again).
- **Ruling (which closes return):** the console's OWN closes return (esc on
  an unfocused console, ctrl+] twice, the session removed or gone). A
  give-way close (a click on a sidebar row, a pane navigation, the viewer's
  back) is the user working in the shown worktree's page: no return —
  reloading back would throw that click away.
- alt+q's page stop, alt+w's hide and a replacing console never return.
- alt+w: windows = the console, the file viewer (to the background), file
  history, blame. Any other layer on top or a focused input = ignored. The
  diff layout is the page, not a window. Unreachable worktrees are not
  skipped (the reroot's own error shows).
- Hosted pages: `/api/reroot` asks the terminal, which switches too (as
  "switch here" does today).
