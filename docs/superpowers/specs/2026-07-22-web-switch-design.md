# Web increment 2: branches sidebar + SmartSwitch over the op transport — Design

**Date:** 2026-07-22
**Branch:** `feat/web-switch`, based on `web-dev` (per the web-dev integration
rule: web features branch from and merge back into `web-dev`; main is never
touched by the web experiment).

## Goal

Build the **op transport** — the streaming spine every future web write
operation rides on — and prove it end-to-end with `engine.SmartSwitch`
driven from a new branches sidebar. This is the increment the stage/unstage
design explicitly deferred ("the transport arrives with the first forking
op").

## Context / prior decisions

- The engine contract: an `Operation` streams `Event`s
  (`Progress`/`GitLine`/`DecisionNeeded`/`Timing`/`Done`) and resolves forks
  through a `Decider` that only ever picks from an option list. Operations
  never block on a human — the decider's channel send selects on `ctx.Done`.
- `SmartSwitch{Branch}` auto-stashes a dirty tree, switches, restores; its
  one fork is `stash-pop-conflict` (options `keep`/`abort`) when the restore
  conflicts on the new branch. Progress steps: `stashing` → `switching` →
  `restoring changes`. Refusals (e.g. branch checked out in another
  worktree) surface as op errors, not decisions.
- **SSE, not WebSocket** (user-approved): gg's web stack is stdlib-only and
  WebSocket would be its first external dependency. Server-Sent Events give
  server→client streaming from `net/http` alone; client→server answers are
  plain POSTs. Functionally equivalent for this use.
- Existing plumbing this builds on: `writeGuard` (JSON content type else
  415, loopback Origin else 403), `hostGuard`, `isGitArgSafe`, the
  `runOp`/`noDecider` pattern in `internal/web/stage.go`, and the drill-in
  SPA layout (`#panes.solo` list / `#panes.detail`).

## A. Op transport (`internal/web/oprun.go`)

### Lifecycle

- `POST /api/op` body `{"op": "switch", "branch": "<name>"}` (writeGuard):
  - `op` must be `"switch"` (the only op this increment) → else 400.
  - `branch` must be non-empty and `isGitArgSafe` → else 400.
  - If an op is already live → **409** `{"error": "an operation is already
    running"}`.
  - Otherwise: start the op in a server goroutine, respond **202**
    `{"op_id": "<id>"}`.
- `GET /api/op/{id}/events`: SSE stream (`Content-Type: text/event-stream`,
  `Cache-Control: no-cache`, flush after every event). Sends the op's full
  buffered history first (replay — a client connecting a beat late misses
  nothing), then live events, and closes after the terminal event. Unknown
  id → 404 (JSON error, not SSE).
- `POST /api/op/{id}/decide` body `{"option": "<opt>"}` (writeGuard): feeds
  the parked decider. Unknown id → 404; op not waiting on a decision → 409;
  option not in the pending request's option list → 400; ok → 200 `{}`.

### Wire events

Each SSE message is `data: <json>\n\n` with a `type` discriminator
(EventSource `onmessage`, no named events):

```json
{"type":"progress","step":"switching","detail":"main"}
{"type":"gitline","raw":"Switched to branch 'main'"}
{"type":"decision","id":"stash-pop-conflict","prompt":"Restoring your changes conflicts with main","options":["keep","abort"]}
{"type":"done","ok":true,"changed":true,"summary":"switched to main"}
```

- `progress` ← `engine.Progress{Step, Detail}`; `gitline` ←
  `engine.GitLine{Raw}`; `decision` ← `engine.DecisionNeeded.Request`
  (`ID`/`Prompt`/`Options` — English protocol strings, like the CLI).
  `engine.Timing` is not forwarded (observability, not UI).
- `done` is synthesized when `domain.Execute` returns: `ok` = err == nil,
  `changed` = `Result.Changed`, `summary` = `Result.Summary`, plus
  `"error": "<err>"` when err != nil. `done` is ALWAYS the stream's last
  event, even on failure — the client keys its refresh on it.

### Server plumbing

- `opRun` struct: id, buffered `[]wireEvent` history + per-subscriber fan-out
  (mutex + `chan wireEvent` per attached SSE client), `decide chan string`,
  `pending *engine.DecisionRequest` (nil when not parked), done flag.
- `Server` gains `opMu sync.Mutex` + `cur *opRun`. Exactly one live op; a
  finished op's record is kept (for late SSE reads) until the next
  `POST /api/op` replaces it.
- The op goroutine runs `svc.Execute(ctx, op, events, webDecider{run})`:
  a pump goroutine drains the events channel into the run's buffer/fan-out
  (the `runOp` pattern, but forwarding instead of discarding).
- `webDecider.Decide` records `pending`, emits nothing itself (the
  `DecisionNeeded` event already went through the events channel), then
  `select`s on: the answer channel, `ctx.Done()`, and a **5-minute
  timeout** — timeout/cancel returns an error, which the engine surfaces as
  the op's error (an abandoned modal cannot wedge the repo gate).
- Op context: `context.Background()`-derived with cancel stored on the run
  (severed from the HTTP request that started it — the op must survive that
  request ending). Server shutdown is out of scope for the probe (matches
  the existing `Serve` simplicity).

### Feed invalidation (correctness fix)

`Server.feedFor` caches one `domain.CommitFeed` forever. A successful switch
changes HEAD, so the cached feed would keep serving the OLD branch's
commits. After any op whose `done.changed` is true, the server resets the
feed (`s.mu.Lock(); s.feed = nil`) so the next `/api/commits` rebuilds. The
reset lives in the op-finish path (server-side), not in the client.

## B. Branches API + sidebar

### `GET /api/branches` — new file `branches.go`

`domain.Branches(ctx)` → sorted as returned (git's order):

```json
{"branches":[{"name":"main","upstream":"origin/main","ahead":1,"behind":0,
  "is_head":true,"hash":"<full>","time":1753142400}]}
```

### SPA sidebar

- List screen only: `#panes.solo` becomes `grid-template-columns: 230px 1fr`
  with a new `#branches-pane` (first grid child; hidden in detail mode, and
  collapsible with the **`b`** key — collapse = back to `1fr`, remembered in
  `state.sidebar`).
- Rows: branch name (HEAD row marked `✓` and styled, like the TUI's current
  branch), dim `↑N ↓M` ahead/behind badges when non-zero. Loaded at boot and
  after every `done{changed:true}`.
- **Click a non-HEAD branch → `POST /api/op {op:"switch", branch}`** →
  attach `EventSource` to `/api/op/{id}/events`. Clicking the HEAD row does
  nothing. While an op is live, branch clicks are ignored (the server would
  409 anyway).
- **Op status line**: a footer-left segment shows the live progress
  (`⟳ switching main…` from `progress` events, then the `done` summary or
  error; errors stay visible until the next op). Reuses the footer bar — no
  new chrome.
- **Decision modal**: on a `decision` event, a centered overlay renders the
  prompt and one button per option (English protocol values, like the CLI).
  Clicking POSTs `/api/op/{id}/decide` and closes the modal. **Esc clicks
  the option named `abort` if present** (the TUI's esc rule); no `abort`
  option → esc does nothing. The modal blocks other interaction (overlay).
- After `done{changed:true}`: refetch `/api/repo` (header branch),
  `/api/branches`, `/api/status`, and reload commits from scratch
  (`loadCommits(false)` — the server has reset its feed; the client resets
  `state.rows`, cursor to 0).
- **The client closes its `EventSource` on receiving `done`** — EventSource
  auto-reconnects when the server ends the stream, which would replay the
  buffered history and double-fire `done` handling otherwise.

## C. Security

- `POST /api/op` and `POST /api/op/{id}/decide` sit behind `writeGuard`.
- The SSE GET is read-only — `hostGuard` (already global) suffices.
- `branch` passes `isGitArgSafe` before reaching git argv.
- Decide options are validated against the pending request's option list —
  a frontend bug can never inject a free-text answer (decisions are
  option-lists only, the project-wide rule).

## D. Testing (Go, real git in t.TempDir(), httptest)

1. **Clean switch end-to-end**: two-branch repo → POST op → read the SSE
   stream to `done{ok:true,changed:true}` → `git rev-parse --abbrev-ref
   HEAD` (test-side) confirms; `/api/commits` afterward reflects the new
   HEAD (feed reset verified by subject set).
2. **Forced stash-pop conflict**: branch A dirty edit on a file whose
   content differs on branch B such that the stash pop conflicts → stream
   yields `decision{id:"stash-pop-conflict"}` → POST decide `"abort"` →
   stream ends with `done` (op error propagated, `ok:false`); repo state
   sane (still on B, stash preserved — assert via `git stash list`).
3. **Replay**: attach the SSE reader only AFTER `done` → still receives the
   full buffered history.
4. **Busy**: second POST /api/op while one is parked on a decision → 409.
5. **Validation**: unknown `op` → 400; empty/`--evil` branch → 400; decide
   with an option outside the list → 400; unknown op id → 404 (events and
   decide).
6. **Decision timeout**: with the timeout shrunk via a test seam (field on
   Server, default 5m), an unanswered decision ends the stream with
   `done{ok:false}`.
7. **Branches endpoint**: two branches + upstream/ahead-behind fixture →
   JSON fields as spec'd, `is_head` on the right row.

JS untested-by-design (probe convention): Playwright pass for sidebar,
op status line, and modal (forced-conflict scratch repo), read-only against
real repos.

## E. Out of scope

- Any op other than `switch` on the transport (the registry is built to add
  them — one switch statement — but none are wired).
- Branch create/delete/rename; remote branches in the sidebar.
- Multi-client op streaming guarantees beyond the replay buffer
  (single-user loopback tool).
- Localization of wire strings (English protocol, like the CLI).
- Graceful server-shutdown draining of a live op.
