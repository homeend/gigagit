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

User feedback on the first cut (a switch = `/api/reroot` + a page reload,
the console carried through it): the reload freezes the page, shows the
switching veil and repaints everything. And the git-lock bar showed almost
all the time ("index.lock (0s old)").

### In-place worktree switch

- A switch to a LISTED worktree of this repository (`switchTarget`: the
  Worktrees list's rows) never reloads: `doReroot` posts `/api/reroot` and
  then `softSwitch(path)` re-populates only what a worktree changes. A
  switch to another repository keeps the reload (everything changes).
- `softSwitch`, at once (before any fetch): `state.worktree`, the header
  path and branch, the ✓ marks (`is_head` flipped from the Worktrees row's
  branch; Branches and Worktrees re-rendered), the commit box draft swapped
  for the arriving worktree's (one per worktree, the TUI's slots), the
  attention bands dropped, `opLine("switching to <name>…")`.
- Then, in parallel: the header (`loadRepo`), status, the worktree-side
  sidebar (branches — ahead/behind and HEAD —, worktrees, the HEAD reflog;
  NOT tags, remotes, stashes, bookmarks, shelf: repo-wide), health, the
  note counts (working reviews are per worktree), the notification centre
  (the server resets its session), the lock bar; then the commit feed
  (`loadCommits(false, true)`: the server dropped its feed), the cursor
  kept on its commit, `reconcileStatusView` — `refreshAfterOp`'s rules.
- Previews, PRs, tags, remotes: repo-wide, not re-read.
- The live stream: the server's "switched" message and the next hello name
  the new worktree. `followSwitch` is the one entry: a listed worktree →
  `softSwitch` (idempotent on the path) and the hello goes on as a
  reconnect (`scheduleFull`, `viewerHello`); another repository → the
  reload. This also covers a hosted terminal switching on its own.
- No veil, no reload, no carry: the console simply stays open.
- Windows over the page (viewer, history, blame) stay as they are — the
  TUI parks them per worktree; here alt+w closes them, a walk leaves them.

### Rulings kept from the first cut

- **Op running:** the console opens in place and the line says "an
  operation is running — switch once it has finished"; no queue.
- **A refused switch** (a busy hosting terminal, a failed reroot): the
  console shows in place, the error line stays.
- **Which closes return:** the console's OWN closes (esc, ctrl+] twice, the
  foot's close button, the session removed or gone). A give-way close (a
  click on a sidebar row, a pane navigation, the viewer's back) does not.
- alt+q's page stop, alt+w's hide and a replacing console never return.
- alt+w: windows = the console, the file viewer (to the background), file
  history, blame; any other layer on top or a focused input = ignored. The
  diff layout is the page, not a window.

### The lock bar

- A lock git holds right now (the page's own status scan on a big repo, an
  agent's git) is not a stranded one. The bar shows a lock only when a
  second probe ~3s later finds the SAME lock (same path, same mtime — its
  age grew by the elapsed time); a live git's lock is gone or replaced by
  then (`confirmedLocks`, pure).
- No probe at module load (it raced boot's own status scan): the first runs
  after the first status lands; then on focus and after a switch, as before.
