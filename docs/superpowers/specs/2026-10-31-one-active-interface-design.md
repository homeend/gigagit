# One active interface — Design Spec

**Date:** 2026-10-31
**Status:** Draft, awaiting review
**Scope:** A `gg` process shows either its terminal UI or its hosted web page,
never both. The next spec, web workspaces, builds on this one.

---

## Intent

Today the terminal and the page it hosts work in parallel: both take input,
both refresh, and each can start work the other then has to reconcile. The
user wants one interface in charge at a time, and a deliberate, symmetric
switch between them. The other interface says plainly that it is not in charge
and offers the way back.

## Goals

- Exactly one of TUI and web UI is **active** in a process. The other is
  **blocked**: it shows a notice with two buttons and takes no other input.
- `gg` starts with the TUI active. `gg --web` starts with the web UI active.
- **Switch to web UI** (TUI) and **Switch to TUI** (web) are confirmed commands
  that mirror each other.
- The blocked side costs nothing: no refresh, no git calls.

## Non-goals

- Standalone `gg web` changes. It has no terminal, so it has no block and no
  Switch to TUI.
- Coordinating separate processes. `gg` in one shell and `gg web` in another
  stay independent, as two `gg` TUIs do.
- Carrying windows across a switch. Open windows do not reappear on the other
  side; both sides show the same repository and worktree (§4).
- Web workspaces, including `alt+s` / `alt+S` over members. Those are the next
  spec. Until then a page behind a workspace shows the member the terminal was
  on, without grouped lists, as it does today.

## Anti-goals (a failure even if it works)

- Any way to end up with nothing usable: both sides blocked, a blocked terminal
  with no reachable page, or a page that says "press esc in the terminal" while
  the terminal is blocked.
- A switch that stops or restarts agents, consoles or AI tasks.
- A blocked side that keeps polling git.
- A visible full-page reload when a tab that was already showing gg becomes
  active again.

---

## 1. What the user sees

### Blocked terminal

The whole screen is replaced:

```
                       Web UI is active

               http://127.0.0.1:43127/

               > Activate TUI
                 Quit
```

- `↑`/`↓` or `tab` move, `enter` picks, and both rows are clickable. `q` is Quit
  and `ctrl+c` quits as it does everywhere.
- `esc` does nothing (my call): a stray esc must not take the interface away
  from someone working in the browser, and the two buttons are the exits.
- While the page is starting, the address line says `starting…`.
- `alt+U` emergency unlock keeps working, as it does over any window.
- A refused activation (§3) shows its reason under the buttons, and the block
  stays.

### Blocked page

Every tab of the page shows a full-window notice: **TUI is active**, with
**Activate web UI** and **Quit** buttons. No other key or click reaches the
page. A tab opened while the TUI is active shows this notice straight away.

### Commands

- TUI palette: **Switch to web UI** replaces **Open web UI in browser**, which
  is removed.
- Settings → Web page: the **Open in browser** row is renamed **Switch to web
  UI** and runs the same command, with the same confirmation.
- Web palette and ☰ menu: **Switch to TUI**, on a hosted page only.
- Each confirmation is one question: "Switch to the web UI? The terminal stays
  blocked until you switch back." / "Switch to the TUI? This page stays blocked
  until you switch back."
- The buttons on a blocked screen do not ask again. Quit does ask when live work
  would end (§5).
- The TUI strings go through `i18n.T` in all four bundles. The commands appear
  in `?` help on both sides.

## 2. Launch

| Launch | Page served | Active | Browser opened |
|---|---|---|---|
| `gg` | no | TUI | no |
| `gg`, `[web] serve` on | yes | TUI (page shows its block) | no |
| `gg --web`, `--web-addr` | yes | web UI | yes |
| `gg web` | yes, standalone | web UI, no block exists | as today |

`--web` now opens the browser because the terminal is blocked, so the page is
all there is to look at. The Settings serve toggle keeps its meaning and text.

With `--web` the terminal is blocked from the first frame, with its address line
at `starting…`. The launch load still runs once. Prompts the launch can raise
(workspace question, paused operation, slow filesystem) wait until the TUI is
first activated; they never appear behind the block.

**A page that fails to start never blocks.** A bind error or another live page
on this worktree (`ErrPageLive`) leaves or returns the TUI active, with the
error on the status line.

## 3. Switching

One rule for every route — a confirmed command, a blocked screen's button, and
`gg open --web`.

**Refused while the side giving up control is busy**, with its reason:

- TUI giving up control: an operation running, a decision waiting, a terminal
  handover in progress (`opsIdle`, `m.modal`, `m.proc`).
- Page giving up control: an operation in flight (`opInFlight`).

AI tasks and agent sessions never refuse a switch. A refusal is shown where the
request was made: a blocked screen's reason line, or the status line otherwise.

**Switching to the web UI:**

1. The TUI closes its open popups and windows, and cancels anything being typed.
   This loses what `esc` would lose and nothing more.
2. If the terminal is looking at a worktree other than its own (a look through
   `alt+w`, `alt+a`, …), it adopts that worktree first, so the page opens where
   the user was. It uses the same path as enter on a worktree row.
3. The page is started if it is not running, and stays up from then on.
4. The active side flips and every tab is told.
5. The browser opens only if no tab is connected. An open tab flips in place,
   and a second tab per switch would pile up. The blocked terminal's address is
   the way back if the browser never appears.

**Switching to the TUI** mirrors this: every tab closes its open windows and
popups, losing what `esc` would lose (Settings' unsaved bar included), and shows
its block. Windows left open would come back stale after the TUI changed the
tree. The terminal then reloads and redraws, and resends its size to any console
it shows, because the page may have resized that session's terminal.

**The agent window follows the switch (added 2026-10-31).** The session whose
console is on screen crosses a flip in both directions; the side left hides its
own (the session keeps running). None shown means none on arrival, and a session
gone since is skipped without a message. A page tab reports what it shows
(`POST /api/ui-console`, refused while blocked, so closing its windows at the
block can't overwrite it); the terminal hands over with `SetShownConsole`.

**A tab becoming active again** re-reads its state in place, without a visible
reload. The reads are `softSwitch`'s, but they need a new entry point, because
`softSwitch` returns early when the path has not changed. A tab that booted
blocked has nothing on screen yet and simply boots.

## 4. Same place on both sides

The repository and worktree follow the switch; nothing else does.

- The page's own repository and worktree switches still ask the terminal, even
  while it is blocked, so the terminal lands where the page was when it returns.
- `onWebSwitchRequest` refusals can no longer name a window, because the switch
  closed them. Only an operation can refuse, and a blocked terminal has none.
- A terminal re-root still moves the page (`webRerootCmd`), so a blocked page's
  next activation is already on the right repository.

## 5. Quit

Quit ends the process from either side, through the existing guards:

- Blocked terminal: `tea.Quit` meets `quitFilter`. With live work, the quit-mode
  sessions popup opens over the block; closing it returns to the block.
- Blocked page: the existing ☰ quit (`quitFromMenu`) confirmation, naming the
  live work, then the terminal's quit path.

## 6. What the blocked side does

**Blocked terminal.** `Update` keeps running, because it serves the page's
switch, session-start and quit requests. It stops the background refresh lane,
watcher-driven reloads, member fetches and polling, and skips drawing the
panels. Activation runs a full refresh. Sessions, consoles, tasks and the agent
channel carry on: those are the agents' work, not an interface.

**Blocked page.** The server refuses requests with `409` and a marked body, in
one wrapper around `Server.Handler`, so a stale tab or script cannot act behind
the notice. The allowlist is:

- `GET /` and `/static/`
- `/api/ping`, `/api/events`, `/api/repo` (which gains the active flag)
- the new activate endpoint, and `/api/quit`

The hub's watcher and interval ticker stop; the event connection stays open to
carry the flip. The server-side writes `Sessions` and the terminal share keep
working through the terminal's own routes.

The page must not treat these `409`s as errors. It reads the flag from
`/api/repo` first and holds its boot until it is active.

**Standalone.** A standalone server is always active: the wrapper and the
commands are gated on `hosted`.

**AI task results wait for the side that started them.** A task keeps running
through a switch and shows as finished in the AI tasks list on the active side.
What applies its result to the screen (filling the commit box, opening a review,
a conflict result, a notice: `onTasksChanged` in the terminal, the starting tab's
`launchTasks` on the page) does not run while that side is blocked. It runs once
on activation, against the task records as they are then, so nothing is lost and
nothing draws or calls git behind the block.

## 7. Agents and the CLI

- **Presence.** `steer.Presence` gains an `Inactive` field. The blocked side
  keeps touching its presence with it set (so liveness and the foreign-page check
  still work). `gg session status` reports it. While the web UI is active, the
  terminal's presence also carries the page's URL, in every inbox the terminal
  still answers (its kept inboxes, `steer_kept.go`).
- **Routing.** `gg session` without `--to` reaches the active side. This must
  hold for an agent started before a switch: its `GG_INBOX` may name a worktree
  with no `web.json`, so when the presence it finds is `Inactive` with a URL, the
  CLI posts to that URL. An agent's overview therefore reaches the browser the
  user is in. `--to <blocked side>` is refused: "the web UI is active in this gg"
  / "the terminal UI is active in this gg". A blocked side that still receives a
  command answers with the same failure.
- **`NotifyReload`** stays best-effort. A blocked side ignores it, since
  activation refreshes everything.
- **`gg open --web`** with a live TUI posts `serve`, which now means "make the
  web UI active" with no confirmation, since the command is itself the explicit
  ask. It is refused under §3's rules; the CLI prints the reason and exits 1. It
  then sends the link as today. `routeFor` must send this case through `serve`
  even when a page is already listening but blocked.
- **`gg open` without `--web`** while the web UI is active delivers the link to
  the active page and flips nothing, because the reviewing skills tell users to
  run it. `gg open --background` behaves as today. A bare repository link still
  launches a separate TUI, as separate processes stay allowed.
- **`gg_ui_state`** gains a field naming the active interface. The TUI snapshot
  it reads stays as last written, and while the web UI is active agents are told
  it describes a blocked terminal. Publishing the page's own state is out of
  scope.
- Session docs, notes and overviews are files and keep being written. Only the
  focus command is routed.

## 8. Shape of the change (my call, the builder may refine)

- **`WebHost`** gains `SetActive(bool)`, `SetActivator(fn)` (the page's request
  to become active, or to activate the TUI, over the request/reply channel
  pattern `switcherFor` and `quitterFor` use) and `Clients() int` (connected
  tabs). The TUI's Model owns the state; the host holds a copy for the wrapper.
- **`tui`**: a blocked gate high in `Update` for key and mouse messages, and a
  branch early in `View`. Whether it is a layer is the builder's choice, as long
  as `quitHeldMsg`'s popup can appear over it and nothing else can.
- **`web`**: the flip fans out as `liveMsg` `Reason: "ui_active"`, handled in
  `live.js` next to the existing `gg:quit` handling. The notice is a new element
  with its own `#id.hidden` rule.
- `m.quiet` keeps headless from serving. Tests drive the state through the fake
  `WebHost` in `webhost_test.go`.

## 9. Testing

- **TUI unit:** the switch confirm; each refusal; layers closed; a look adopted;
  a start failure stays active; blocked keys swallowed; the buttons by key and
  mouse; quit with live work; no refresh lane or watcher reloads while blocked,
  and a refresh on activation; palette and Settings rows; `--web` blocked from
  the first frame.
- **Web unit:** the wrapper's allowlist in a table; the `409` shape; standalone
  never gated; the flip event; the activate endpoint's refusals; JS pins.
- **Steer and CLI:** routing with `Inactive` on each side, `--to` refusals, `gg
  open --web` flipping, refused, and over an already-serving blocked page.
- **e2e:** a golden of the blocked terminal, and of the palette without the old
  row.
- **Browser probe** (Firefox and Chromium, scratch XDG dirs): flip both ways with
  one tab and with two, assert visibility by geometry, assert no git process
  spawns while blocked, boot a tab while blocked, return without a reload.
- **Race gate** before merge.

## 10. Docs

`CHANGELOG.md`; `README.md` (the web section near line 1974, `--web`);
`docs/web-tui-parity.md`; `docs/CLAUDE-details.md`; the `tui` and `web` rows of
`CLAUDE.md`; `internal/agentskill/using-gg.md` for `gg open --web` and `gg session
--to` (bump its version).

Removal fallout: `command_palette.go:74`, `webhost_test.go:225,229`,
`command_palette_test.go:104`, `webRowOpen`, and the old strings in the four
bundles.

## 11. Decisions

**The user's:** one active interface; the commands, their confirmation, the two
buttons on each side; the removal and the Settings rename; `gg --web` starting
web-active.

**Stated in chat and not objected to:** `--web` and a switch open the browser only
when no tab is open; `[web] serve` leaves the TUI active; a switch closes popups;
busy sides refuse; work keeps running and Quit asks; the blocked side is idle and
its server refuses; `gg open --web` flips without a confirmation; `--to <blocked
side>` is refused; standalone `gg web` is unchanged and separate processes stay
allowed.

**Mine, in this document, to check while reading:**

- `esc` does nothing on the blocked terminal.
- A look is adopted before switching to the web, so the page opens where you were.
- The blocked screen's buttons do not ask again.
- `gg open` without `--web` delivers the link to the active page without flipping.
- An agent started before a switch still reaches the browser through the blocked
  terminal's presence.
- A page that cannot start leaves the TUI active.
- The launch prompts wait for the first activation.
- The page closes its windows when it gives up control, mirroring the terminal,
  and half-typed text on either side is lost as `esc` would lose it.
- AI task results wait for the side that started them, and show as finished in
  the AI tasks list meanwhile.
- `gg_ui_state` names the active interface and does not publish page state.
