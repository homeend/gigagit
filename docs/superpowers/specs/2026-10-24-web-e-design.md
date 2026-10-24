# gg web E — worktree claims and reservations, the quit guard

Parity bundle E from `docs/web-tui-parity.md` (rows "Worktree claims +
reservations" and "Quit guard"). The TUI is the spec; where a browser cannot
do what the terminal does, a ruling says what stands in.

## 1. Worktree claims and reservations

TUI (`internal/tui/worktree_marks.go`, `view.go`, `agent_start_popup.go`):

- Each Worktrees row carries a mark column before the branch:
  `⚑ <agent>  ` for a live claim (agent name, falling back to `agent`) and
  `⊘  ` for a reserved worktree, both when both hold.
- The bottom bar, on the selected row: `⚑ claimed by <agent> since
  <YYYY-MM-DD HH:MM>` (local time) plus ` · <note>` when the claim has one;
  a reserved, unclaimed row says `⊘ reserved: agents never take this
  worktree`.
- The row's `.` menu, after Start agent / Open terminal: `Release claim
  (<agent>)` while claimed, then `Unreserve` (reserved) or `Reserve (no
  agents)`.
- Release asks first: `<agent> is working in <dir>. Release its claim?` with
  `Release` / `Cancel`, Cancel highlighted. Release calls
  `ReleaseWorktree(path, "", force=true)`; success says `released the claim
  on <dir>`, failure `release claim: <err>`.
- Reserve / Unreserve writes `[agents] reserved` through
  `AgentsConfig` + `SetWorktreeReserved` (the main-anchored repo config);
  failure says `reserve: <err>`.
- Marks come from `WorktreeMarks(wts, AgentsConfigFrom(wts).Reserved)`.

Web today: no marks, no rows.

Port:
- `GET /api/worktrees` rows gain `reserved` (bool) and `claim`
  (`{agent, since, note}`, omitted when unclaimed), read with the same
  `WorktreeMarks` call. Best-effort: a config read failure costs the
  reserve marks, never the list.
- The sidebar Worktrees row draws the TUI's mark column before the label.
- **Ruling (the bar):** the web sidebar has no selected-row bottom bar; the
  row's tooltip (`title`) carries the TUI's bar text on its own line under
  the path. Cost if wrong: a hover instead of a glance.
- The worktree menu appends the TUI's rows after `Start agent in …` /
  `Open terminal in …`, in the TUI's order and words.
- Release opens the web's decision dialog (`openPrompt`-style confirm, the
  Cancel button focused) with the TUI's question, then
  `POST /api/worktree/release-claim {path}`.
- Reserve / Unreserve: `POST /api/worktree/reserve {path, on}`.
- Both handlers: `writeGuard` + `MaxBytesReader`; the path must be one of
  the repository's worktrees (`SameCheckout` against a fresh list — the
  domain's `worktreeAt` refuses others with `ErrUnknownWorktree`, mapped to
  400). Success fans out a `worktrees` live change so every tab redraws.
  The page's op line shows the TUI's success / failure words.
- Help: the worktree help entry describes ⚑ / ⊘ and the rows.

## 2. Quit guard (standalone `gg web`)

TUI (`quit_guard.go`, `sessions_popup.go`, `run.go`): a quit while
`liveWork() > 0` (live sessions plus live AI tasks without a running session
of their own) is held and opens the sessions popup in quit mode: the header
`agents and AI tasks still running: N — quit gg?`, the Agents and AI tasks
tabs only (no Open files), the tasks tab's own keys off, and
`[Q] kill all and quit  [esc] cancel`. `Q` kills tasks first ("the records say
cancelled"), then sessions, bounded by `killAllGrace` (5 s), then quits.
Whatever ends the program, a final `Tasks().KillAll` + `Sessions().KillAll`
within the grace runs as a safety net.

Web today: `Serve` waits for `os.Interrupt`, closes the host, returns.
Sessions and tasks the page started are not ended.

Port:
- `domain.LiveWork()` — the TUI's `liveWork` moved to domain unchanged; the
  TUI calls it.
- **Ruling (where quitting happens):** a standalone `gg web` quits on Ctrl+C
  in its terminal (a browser tab closing is not a quit). The first Ctrl+C
  with `LiveWork() > 0` is held: stderr says `gg web: agents and AI tasks
  still running: N — press Ctrl+C again to end them and quit`, and every
  open page opens the switcher in quit mode. A second Ctrl+C while the ask
  is up confirms. With no live work, Ctrl+C quits at once (as today).
- Quit mode in the page (the ctrl+\ switcher): header `agents and AI tasks
  still running: N — quit gg web?`; tabs Agents and AI tasks only; the AI
  tasks tab's row keys are off; foot adds `Q kill all and quit` and
  `esc cancel` buttons. `Q` → `POST /api/quit {confirm: true}`; esc / the
  cancel button → `POST /api/quit {confirm: false}`, which takes the ask
  down in every tab (the next Ctrl+C asks again — the TUI's esc).
- `GET /api/quit` → `{asking, live}`; a `quit` live reason tells pages to
  re-read it (a page loaded during the ask shows it too).
- `POST /api/quit` is refused (409) unless the ask is up, and on a hosted
  page (the terminal owns quitting) — a page can never stop the server
  unprompted.
- Confirmed (either way): stderr `gg web: ending agent sessions…`,
  `Tasks().KillAll` then `Sessions().KillAll` within 5 s, then the host
  closes. The safety-net kill runs after the host closes on every exit path
  of `Serve`.
- **Ruling (headless tasks and Ctrl+C):** a headless task's process shares
  `gg web`'s process group, so the terminal delivers the same SIGINT to it;
  the ask protects agent sessions (PTYs in their own session) and queued
  tasks, while a running headless child may already have ended. Changing
  the capture runner's process group is out of scope. Cost if wrong: a
  headless task interrupted on the first Ctrl+C, recorded as failed.
