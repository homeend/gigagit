# Web attach — agent consoles in the gg web page, and session states

Date: 2026-09-28 · Branch: `feat/web-attach` · Status: design agreed in
brainstorming, spec under review.

Builds on agent sessions (`2026-09-24-agent-sessions-design.md`, merged; its
point 10 reserved this work), AI tasks in sessions
(`2026-09-24-ai-tasks-in-sessions-design.md`, merged; rulings 1–9 stand) and
open files (the web viewer, `ctrl+]` / `ctrl+\` on the web).

## Goal

The `gg web` page shows the agent sessions this gg process owns, opens one as
a live console, forwards typing to it, and can start, kill and remove
sessions — the TUI's console and `ctrl+\` popup, in the browser. Both
frontends also learn what an agent is doing (working, idle, asking a
question, stalled) and tell the user when it needs them.

Success: the user starts `claude` under a worktree from the web sidebar,
types to it, steps out to look at the diff while it edits, gets a toast
"claude needs your input", opens the console from `ctrl+\`, answers, and
later sees the same session, same screen, in the TUI — without either
frontend restarting anything.

## Rulings (agreed in brainstorming — do not re-ask)

1. **Scope C.** Watch and type; start (agent or terminal), kill and remove
   from the web; interactive AI-task sessions appear and open like any
   session. Headless tasks stay in their existing web lanes (read-only list
   in the popup's AI tasks tab).
2. **Rendering A: the server's screen, repainted.** No xterm.js. The page
   paints the emulator's grid from styled runs the server sends; every frame
   is the whole truth, so nothing can desync. No scrollback beyond the grid,
   no mouse to the agent (the TUI console has neither).
3. **Input A: keys over POST.** Structured key events, batched while a
   request is in flight; paste as text. No WebSocket (stdlib has none).
4. **Size A: the focused viewer owns cols×rows.** Unfocused viewers never
   push a size and scroll when the grid does not fit. Two TUI changes
   follow: an unfocused docked console stops pushing on window resize, and a
   console pushes its size when it gains focus or is maximized.
5. **Keys A: the TUI's two keys, one popup.** `ctrl+\` becomes the tabbed
   popup on the web (Agents · AI tasks · Open files). `ctrl+]` steps out of a
   focused console. `m` maximizes an unfocused console (Chrome owns
   `ctrl+t`). ctrl+w, ctrl+t, ctrl+n, ctrl+tab, ctrl+shift+…, F5/F11/F12 stay
   with the browser and are never faked; the focused footer says so.
6. **Placement A: a layer over the panes**, sidebar kept; maximize hides the
   sidebar. Session sub-rows under worktrees in the sidebar (and under
   branches that show a worktree, as the TUI's Branches tab does), with the
   `.` menu rows Start agent / Open terminal / Open / Kill / Remove.
7. **Transport 1: a dedicated SSE stream per open console**, not the live
   hub (the hub drops everything while an op runs, and every tab would get
   every console's frames).
8. **Session states** (the erbrus screen-state design, ported): the
   architecture carries state from plan 1 (the `state` field is on the wire
   from the first plan); the detection itself is plan 3. Nothing
   auto-answers a dialog.
9. **Kill parity.** Killing a task-backed session goes through the session
   manager, as the TUI does; the task record follows. Cancelling from the AI
   tasks tab goes through the task manager.

## Architecture

```
 internal/agentstate ── Tail · Classify · DefaultRules(agentID) · Compile · StepDuration · DialogOptions   (DAG leaf, plan 3)
 internal/agentsession ── + ScreenRuns() · Text() · LastOutput()                                          (leaf)
 internal/domain ── Sessions() (as today) · SessionStates() (plan 3) · key table shared with the TUI · aliases
 internal/web ── sessions_http.go (list, start, kill, remove) · console_stream.go (frame producer + SSE)
              · console_input.go (keys, paste, size) · static/console.js (painter + layer) · popup tabs · sidebar rows
 internal/tui ── the two size-ownership changes; state badges + notices (plan 3)
```

### `internal/agentsession` additions

- `ScreenRuns() ScreenRuns` — the grid as styled runs, built from the cells
  under `ioMu` (the `cursorLine` precedent): `Rows []RunRow` (one per row,
  `Runs []Run{Text, Fg, Bg string, Attrs}`), `Cols, Rows, CursorX, CursorY,
  CursorVisible, AltScreen`. A run holds only attributes that differ from the
  default; `Fg`/`Bg` are `#rrggbb` (palette indices resolved through the
  console's 16-colour table, so the page has one colour vocabulary); a wide
  glyph is one run, its right-half cell is not emitted. The cursor is not
  painted into the runs (the page draws it), so one frame serves focused
  and unfocused viewers.
- `Text() string` — the plain grid (today's unexported `screenText`).
- `LastOutput() time.Time` — stamped by the reader on every chunk (plan 3's
  stall clock).

### `internal/domain`

- Type aliases for `ScreenRuns`/`Run`; `web` never imports the leaf
  (archtest).
- `SessionKey` — the browser key wire shape and its mapping to the emulator
  key event, **the same table the TUI's `consoleSpecial` uses** (moved to
  domain, the TUI keeps importing it). Wire: `{k, mod, text}` where `k` is
  one of `char enter tab esc backspace delete insert up down left right
  home end pgup pgdn f1…f12`, `mod` is a bitmask shift=1 ctrl=2 alt=4,
  `text` is the character(s) for `char`. Unknown `k` is refused.
- `ClampConsoleSize(cols, rows)` → 20..500 × 5..300.
- `SessionStates()` (plan 3) — process-global like `Sessions()`/`Tasks()`:
  one goroutine wakes on the manager's `Changed()` and a 2 s tick,
  classifies every running session with a non-empty `AgentID` (terminals and
  custom commands are skipped), keeps `SessionState{State, Since, StepFor,
  Stalled, Options}`, exposes `Get(id)`, `Changed()` (coalesced) and
  `Events() <-chan StateEvent` (question entered; idle after working;
  stalled). Rules: `agentstate.DefaultRules(info.AgentID)`, overridden by
  the session command's `screen_working/screen_waiting/screen_question`
  lists when any is set (all three then come from config).
- `StartSession`/`StartTerminal` unchanged; the web hands the same env
  (`GG_INBOX` = the web's steer inbox for that worktree, via the shared
  helper the TUI uses).

### `internal/web` — endpoints

All loopback with the Host/Origin guards; POSTs behind `writeGuard`; ids
are validated against the manager before any work.

| Method | Path | Body / query | Does |
|---|---|---|---|
| GET | `/api/sessions` | — | `{sessions:[{id,label,agent,repo,worktree,state,exit_code,started,agent_state,since}]}` for every session of the process (all repos); grouped client-side repo → worktree |
| GET | `/api/session-screen` | `?id=&tab=` | SSE: `hello` (a full frame + the 16-colour table), then `frame` events with changed rows, `exited {code}`, `gone` |
| POST | `/api/session-input` | `{id, keys:[{k,mod,text}], paste}` | keys then paste, in order; 409 for an exited session |
| POST | `/api/session-size` | `{id, cols, rows}` | clamped; a size the session already has is a no-op |
| POST | `/api/session-start` | `{worktree, tool, approve}` or `{worktree, terminal:true}` | the AI lanes' `needs_approval` 403 pattern; first run runs `EnsureSessionCommands` off-request with a busy notice on the page; returns the new session's info |
| POST | `/api/session-kill` | `{id}` | manager rules (a running session only) |
| POST | `/api/session-remove` | `{id}` | manager rules (an exited session only, else 409) |
| GET | `/api/session-commands` | `?worktree=` | the configured `session` commands for the web frontend with `approved` per row, `found` per binary |

`/api/events` (the live hub) gains a `sessions` event on every manager
change (start, exit, remove, and in plan 3 every state transition) so the
sidebar rows and the popup refresh without polling. It rides `emitSteer`'s
gate-bypassing path: a session exiting mid-op must still show.

### Frame producer

One producer per session with ≥1 attached stream. It waits on the session's
`Changed()`, coalesces **40 ms** (a trailing tick catches the last change),
takes one `ScreenRuns()` snapshot and hands it to each attached stream.
Each stream keeps the last frame it wrote and sends only rows whose runs
differ; a size change, a fresh attach, and a reconnect send the whole grid
(`full:true`). A stream whose write would block is skipped for that frame
and receives a full frame on the next one, so a slow tab never lags behind
or shows garbage. The producer stops when the last stream closes; a session
exit sends `exited` and the producer stays alive while streams remain (the
last screen keeps repainting on demand); `Remove` sends `gone` and closes.

Frame:

```json
{"full":true,"cols":120,"rows":40,"cx":5,"cy":12,"cursor":true,"alt":false,
 "lines":[{"y":0,"runs":[{"t":"$ ls","fg":"#c0c0c0","b":true}]}, {"y":1,"runs":[]}]}
```

Run keys: `t` text, `fg`/`bg` `#rrggbb`, flags `b i u r d s` (bold italic
underline reverse dim strike), present only when set. A partial frame lists
only the changed rows by `y`; an omitted row is unchanged; a row with no
runs is blank.

### Page — `static/console.js`

Pure section (JS-in-Go tests like the other modules): `paintFrame(prev,
frame)` → row HTML per changed row; `keyToWire(KeyboardEvent)` → `{k, mod,
text}` or `null` for a key the page must let through; `gridSize(px, cell)` →
`{cols, rows}`; `isReserved(e)` for ctrl+] / ctrl+\.

Layer (`pushLayer`, `pushFoot`), over the panes, sidebar kept:

- Title: `<label> · <worktree> · running 12m` / `exited (N)` (worktree cut in
  the middle with `elidePath`), the grid size at the right; plan 3 adds the
  state badge.
- Body: a `<pre>`-styled grid, one `<div>` per row, `<span>` per run; the
  cell size measured once from a probe span at the console's font. The
  cursor is a positioned block (solid focused, outline unfocused, hidden
  when `cursor:false`). The grid is drawn at the session's size: a smaller
  box scrolls, a larger one leaves the grid top-left.
- Foot per state — focused: `ctrl+] step out · ctrl+\ sessions` + "ctrl+w ·
  ctrl+t · ctrl+n stay with the browser"; unfocused: `enter focus · m
  maximize · k kill · ctrl+\ sessions · esc close`; exited: `x remove · ctrl+\
  sessions · esc close`.
- Focus: click on the grid or `enter`; `ctrl+]` unfocuses (from maximized:
  back to the pane size, unfocused); `esc` on an unfocused console closes
  the layer and its stream (the session runs on). Opening another session
  replaces the shown one.
- Input: a focused console captures `keydown` on the page; reserved keys
  first; `keyToWire`; keys queue while a POST is in flight and go as one
  array; `paste` sends the clipboard text; an IME composition commits as one
  `char`. An exited console ignores keys.
- Size: on focus, on a resize while focused, on maximize/restore while
  focused → `gridSize` → `session-size` when it changed. Never while
  unfocused.
- Reconnect: EventSource retries; `hello` replaces the grid.

### `ctrl+\` popup (web)

Tabbed like the TUI: `Agents N` · `AI tasks N` · `Open files N`, opening on
the freshest tab (a session or task that changed most recently, else open
files). Agents: grouped repo → worktree (an orphaned worktree group when
the directory is gone), row = glyph (● running, ○ exited; a task-backed
session is ● in the task colour), label, worktree, age or exit code; plan 3
appends the state. Keys: `enter` open, `k` kill (asks once for a running
one), `x` remove (exited only, else a notice), `/` filter, `tab` next tab,
`esc` close. AI tasks: a read-only list from a new `GET /api/tasks`
(`domain.Tasks().List()`: key · agent · state · age — the web's own AI
lanes are operations, not tasks, so nothing existed to reuse); `enter` on a
running interactive task opens its console; a finished task's result opens
in the viewer from plan 2. Open files: unchanged.

### Sidebar

Under every worktree row, one sub-row per session of this repo: `└ ● claude
running 12m` / `└ ○ codex exited (0)`; `enter`/click opens the console.
Worktree `.` menu (via `extraRows("worktree", …)`): **Start agent in
<wt>**, **Open terminal in <wt>**; sub-row menu: **Open**, **Kill**,
**Remove**. Branch rows that show a worktree path get the same two start
rows (the TUI's `branchSessionRows` gate, a0c4fdda). Labels carry no
trailing "…" (user ruling).

### Start agent dialog

`/api/session-commands` → a numbered list (Claude, Claude (yolo), Codex, …)
with `approved` / `approve on start` / `not found` per row; `1–9`/`enter`
start, `esc` cancel. An unapproved command asks the existing approval
question, then starts with `approve:true`. No commands configured: a busy
notice "Detecting installed agents…", then "Added claude, codex to <path> —
edit there or in Settings → External tools", then the list; nothing
detected: the TUI's explanatory notice. Open terminal starts without a
dialog. A started session opens focused.

### TUI changes (plan 1)

- `syncConsoleSize` runs only for a focused (or maximized) console; the
  window-resize path skips an unfocused docked one.
- Gaining focus (enter on an unfocused console, opening a session, `ctrl+t`
  maximize) pushes the console's size.

## Session states (plan 3)

Port of erbrus's `internal/screen` (the user's own code): regexes over the
last 15 non-empty lines of the plain screen in structural order **working →
waiting → question → unknown**, so a phrase quoted in scrollback never
outranks the prompt box on screen. Built-in rules keyed by gg's agent ids
(`claude`, `codex`, `junie`, `agy`, `kimi`, `""` generic), copied from
erbrus's verified sets and re-verified against live screens during the
plan. `StepDuration` parses the spinner's timer. `DialogOptions` reads
numbered (`1. Yes`) and cursor-style (`> Yes, I trust this folder`, radio
glyphs, junie's indented descriptions) lists into `[]Option{Key, Label,
Current}` — carried on the wire now, acted on later.

Watcher rules (`domain.SessionStates()`): unknown never changes anything
(mid-redraw debounce); a transition sets `State`+`Since`; **the first 30 s
after start are not trusted** (sign-in spinners match "working"), so no
notice is posted before then; stalled = `LastOutput` older than 120 s while
working or unknown, live sessions only; states of exited sessions are
dropped. Labels for humans: `working 7m` · `idle 3m` (never "waiting") ·
`needs input` · `stalled · …`.

Surfaces: sub-rows and popup rows (both frontends) show the label, the
attention colour for needs input / stalled; console titles show it live.
Transitions post one notice each: "claude in <wt> needs your input",
"claude in <wt> finished its turn — idle", "claude in <wt> has printed
nothing for 2m — stalled?" — the TUI status line, the web toast and a
`sessions` hub event. Config: `[[tools.command]]` rows of category
`session` accept `screen_working`, `screen_waiting`, `screen_question`
string lists (settingDocs; an invalid pattern is a config warning and the
built-ins apply).

Later, designed for but not built: one-click answering from a sub-row
(`pick:<i>` = ↓/↑ then enter, re-captured at press time), and queuing text
typed while a dialog is up.

## Error handling

| Case | Behaviour |
|---|---|
| tab closes / navigates away | the stream drops; the producer forgets it; the session runs on |
| stream drops on the network side | EventSource reconnects, `hello` refetches a full frame |
| session exits while shown | title `exited (N)`, last screen kept, footer switches; a toast when the console was not focused |
| session removed from another frontend | `gone`, the layer closes with a notice |
| size POST out of range | clamped; a stream never sends one while unfocused |
| input to an exited session | 409; the page already ignores keys |
| start fails (binary missing, spawn error) | error notice, no session |
| kill from the popup or menu | asks once, then kills; "killing …" then the exit toast |
| two tabs, one session | both see the same frames; the last focused one owns the size |
| `gg web` stops | the server-down banner; sessions die with the process, as today |
| emulator panic (existing) | session marked exited with a synthetic line; the stream sends `exited` |

## Testing

- **agentsession:** `ScreenRuns` against a scripted child — colours and
  attributes land in runs, a wide glyph is one run with no phantom half
  cell, cursor/alt flags match `Screen()`; `Text()`; `LastOutput` advances.
- **domain:** the key table maps every browser name to the emulator event
  the TUI's table produces (one shared table test); unknown names refused;
  the size clamp. Plan 3: the watcher with a fake session text feed —
  unknown changes nothing, working→idle posts once, question posts at once,
  the 30 s grace, stalled once, exited dropped, config override honoured.
- **agentstate (plan 3):** erbrus's fixtures verbatim (working, idle with an
  unsubmitted message, a real trust dialog), scrollback phrase + empty
  prompt → idle, blank → unknown, StepDuration on the three timer shapes,
  numbered and cursor dialogs, junie's radio list, Compile rejects a bad
  pattern.
- **web (Go):** the row differ (changed rows only; full after a size
  change; a skipped stream gets a full frame next); `hello`/`exited`/`gone`;
  every POST refused without the guard and with a bad id; input on an
  exited session; the size clamp; `session-start` 403 needs-approval, the
  first-run detect path with a fake detector into an isolated global config,
  the terminal path; kill/remove rules; the `sessions` hub event bypasses
  the op gate. `console.js` pure section pinned by JS-in-Go tests (paint
  output for a frame and a partial frame, key → wire incl. the let-through
  set, gridSize).
- **tui:** no size push while unfocused on resize; push on focus gain and
  maximize.
- **Playwright**, against the unfixed install first, isolated XDG dirs, a
  pre-seeded `[[tools.command]] category="session"` running `sh` (no
  detection, no real agent): start it from the worktree menu → the console
  is visible with the shell prompt → type `echo hello` → echoed → `ctrl+]`
  unfocuses and the foot changes → `ctrl+\` lists it on Agents → a second
  tab shows the same screen → `exit` → `exited (0)` → `x` removes it and the
  layer closes.
- `./test.sh` + `./test.sh race` before each merge; `./build.sh web` after.

## Plans

1. **Watch and type:** `ScreenRuns`/`Text`, domain aliases + key table +
   clamp, `/api/sessions`, the screen stream + producer, input + size POSTs,
   `console.js` painter + layer + foot, the `sessions` hub event, the two
   TUI size changes, sidebar sub-rows with Open, the `ctrl+\` popup tabs
   (Agents, AI tasks read-only, Open files). Sessions are started from the
   TUI in this plan.
2. **Lifecycle:** `/api/session-commands` + start dialog (approval,
   first-run detect, terminal), kill/remove from popup, menu and console,
   Branches-row start entries, exited toast, `m` maximize, docs.
3. **Session states:** `agentstate`, `LastOutput`, `domain.SessionStates()`,
   config lists + settingDocs, badges and notices on both frontends,
   `Options` on the wire.

Between plans 1 and 2 sits the stage `2026-09-28-web-hosted-in-tui-design.md`:
the TUI process serves the page, so both frontends share one session
manager (the plan-1 review found that `gg web` as a separate process could
never see the TUI's sessions).

Docs per plan: CHANGELOG (always), README (the web console, the popup, the
states), CLAUDE.md package map (`agentstate` row in plan 3),
`docs/CLAUDE-details.md` (frame protocol, size ownership, state rules),
memory `agent-sessions-feature.md`.

## Out of scope

xterm.js, scrollback and mouse in the web console; a WebSocket; answering
dialogs from a sub-row and queuing typed text (designed for, plan 4 at the
earliest); CLI/MCP session verbs; sessions outliving the process.
