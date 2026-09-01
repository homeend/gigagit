# Web live refresh — design

**Date:** 2026-09-02 · **Branch:** `feat/web-live-refresh` · **Status:** approved in chat (user chose: follow `[refresh]` config; all TUI sources)

## Problem

`gg web` never refreshes on its own. After an external change to the repo (a
commit from a terminal, a fetch, another gg session, an editor save) the page
updates only when the user presses `r`, runs an op from the web, or re-focuses
the tab (status only). The TUI, by contrast, has a per-source refresh registry:
fsnotify file-watch for refs/reflog/worktrees (`internal/gitwatch`) and an
interval lane for everything else, all driven by the per-repo `[refresh]`
config. The web settings panel edits the interval numbers but labels them
"(TUI)" and exposes no file-watch toggles at all (the four `*_watch` keys are
not on the wire).

Two deliverables:

1. **Option 1 — settings parity.** The four `*_watch` booleans reach the web
   settings payload/writer and render as toggles.
2. **Option 2 — live refresh.** `gg web` runs the same watcher + interval
   machinery server-side and pushes "these sources changed" to every open tab
   over one persistent SSE channel; the page re-fetches just those sources.
   The `[refresh]` settings then apply to both frontends, and the "(TUI)"
   label on the refresh section goes away.

## Non-goals

- Multi-repo: `gg web` serves one repo; the watcher follows re-root.
- Remote change detection: nothing watches `origin`; the `fetch` interval
  covers it, exactly as in the TUI.
- Per-source sidebar fetches: the client's `fetchBranches()` reloads all six
  sidebar lists in one `Promise.all`; a change to any ref-family source reloads
  the sidebar as a whole. Splitting it is a later optimization.
- Notification center refresh on events (it must not poll; unchanged).

## Architecture

```
 .git fs ──fsnotify──▶ gitwatch.Watcher ──Source──▶ ┐
                                                   ├─▶ liveHub.emit(sources) ──SSE──▶ every tab ──▶ live.js ──▶ re-fetch
 interval ticker (due sources; runs fetch /       ┘        (drops while an op            (coalesce, runOnce("refresh"))
   ls-remote server-side, then emits)                       is in flight)
```

All new server code lives in `internal/web/live.go` (+ `live_test.go`); all
new client code in `internal/web/static/live.js`. `internal/web` may import
`internal/gitwatch` (archtest allows it).

### Server: `liveHub`

```go
type liveHub struct {
    mu      sync.Mutex
    subs    map[chan liveMsg]struct{}
    watcher *gitwatch.Watcher
    stop    chan struct{}   // closed by stopLive
    cfg     config.RefreshConfig
    watchOK bool            // gitwatch.Supported(commonDir)
    lastRun map[string]time.Time
}
type liveMsg struct {
    Changed []string `json:"changed"`          // TUI source names
    Reason  string   `json:"reason"`           // "watch" | "interval"
}
```

- **Source names on the wire** are the TUI's `refreshTomlKey` vocabulary:
  `status, branches, remotes, worktrees, tags, reflog, feed`. `fetch` and
  `remote_tags` are actions, never emitted; their effects are emitted as
  sources (`fetch` → `remotes, branches, feed`; `remote_tags` → `tags`).
- **Watch fan-out** copies the TUI table (`internal/tui/watch.go:59-84`):
  `branches → {branches, feed}`, `remotes → {remotes, feed, branches}`,
  `worktrees → {worktrees, branches, feed}`, `reflog → {reflog}`.
- **Watcher construction** copies `startWatchCmd`: `svc.GitCommonDir` →
  `gitwatch.Supported` → enabled sources = eligible ∩ `cfg.*Watch` →
  `svc.GitDir` (fallback common) → `gitwatch.New(gitwatch.Plan(...), 200ms)`.
  Started only when `cfg.Enabled` and at least one watch source is on.
- **Interval ticker**: one goroutine, 1 s tick. A source is due when
  `cfg.Enabled`, its interval > 0 (floored at `min_seconds`, default 10), it is
  not watch-active (eligible ∧ on ∧ supported), no op is in flight, and
  `now - lastRun ≥ interval`. Due handling: `fetch` runs
  `svc.Execute(ctx, engine.Fetch{}, drain, engine.MapDecider{})` then emits
  `remotes, branches, feed`; `remote_tags` runs `svc.RemoteTagsFresh(ctx)`
  then emits `tags`; every other source just emits itself. Ticks are
  sequential (one lane, like the TUI's `bgQueue`); a fetch that takes longer
  than the tick simply delays the next check.
- **Op suppression**: `Server.opInFlight()` (new helper wrapping the
  `s.opMu` / `s.cur.done` idiom used in three places today). While true,
  `emit` drops the message and the ticker skips — the client's post-op
  `refreshAfterOp` reloads everything anyway.
- **Broadcast**: non-blocking send to each subscriber channel (buffer 8);
  a full buffer drops the message for that tab (the tab's next reconnect
  does a full refresh, so nothing is lost for long).
- **Keepalive**: `handleEvents` writes `: ping\n\n` every 25 s so dead peers
  are detected and proxies/browsers don't time the stream out.
- **Lifecycle**: `Server.startLive(ctx)` builds hub+watcher+ticker from the
  effective config; `Server.stopLive()` closes watcher, stops ticker, closes
  every subscriber channel (clients reconnect). Called: from `Serve` after
  `New` (start) and after `httpSrv.Shutdown` (stop, via the new
  `Server.Close()`); from `handleReroot` beside `s.feed = nil` (stop, then
  start on the new service); from `handleSettings` after any write that
  touched `[refresh]` (restart to reload config — the TUI's `watchGen` bump).
  Tests construct a `Server` without `Serve`; `startLive` is explicit so
  handler tests that never call it see no background goroutines.

### Server: `GET /api/events`

Registered through `RegisterRoutes` (`routereg.go`). Read-only GET, no
`writeGuard`. Headers as the per-op SSE handler (`text/event-stream`,
`no-cache`); one `data: {json}\n\n` per `liveMsg`; loop until the request
context ends or the hub stops. On subscribe, the first message is
`{"changed":[],"reason":"hello","live":true|false,"watch":true|false}` so the
client knows whether refresh is enabled at all and whether file-watch is
active (for the settings note).

### Settings (option 1)

- `settingsPayload` gains `RefreshWatch map[string]bool` (`json:"refresh_watch"`)
  with the four eligible sources, and `WatchSupported bool`
  (`json:"watch_supported"`, from `gitwatch.Supported(commonDir)`).
- `settingsWriteRequest` gains `RefreshWatch map[string]*bool`
  (`json:"refresh_watch"`); keys validated against
  `{worktrees, branches, reflog, remotes}`; written with
  `config.SetRefreshWatch(repoPath, src, on)`. The "nothing to set" guard and
  `needRepo` include it.
- Any write touching `auto_refresh`, `refresh`, or `refresh_watch` calls
  `s.restartLive()`.
- `settings.js`: each of the four eligible rate rows gains a `toggleBtn`
  (`data-k="watch:<src>"`, immediate `setOpt`) labelled "file watch"; when
  `watch_supported` is false the toggle stays but the note reads "file watch
  unsupported on this filesystem (9p) — interval is used". The refresh
  heading drops its `(TUI)` span; the footnote becomes "0 = off · per repo ·
  applies to the TUI and to this page". `REFRESH_SOURCES` gains a parallel
  `WATCH_SOURCES` list.

### Client: `live.js`

- On boot (after the first render), `connectLive()` opens
  `new EventSource("/api/events")`.
- `onmessage`: parse; `hello` → store `state.live = {enabled, watch}` and, if
  this is a *re*connect (not the first), run a full `refreshAfterOp(false)`
  (events were missed while disconnected). Otherwise union `changed` into a
  pending `Set` and arm a 150 ms coalescing timer.
- Flush: if `state.op` is set, keep the pending set and retry after the op's
  `done` (hook: `refreshAfterOp` clears pending — it reloads everything).
  Else `runOnce("refresh", async () => { ... })`; if `runOnce` returns null (a
  refresh is already running), keep pending and re-arm the timer for 500 ms.
  The task:
  - `status` → `fetchStatus()`, `reconcileStatusView()`;
  - any of `branches, remotes, worktrees, tags, reflog` → `fetchBranches()`
    and `loadRepo()` (the header branch/ahead-behind);
  - `feed` → remember the cursor row's hash, `loadCommits(false, false)`
    (the server-side reconciling reload — paged history and scroll survive),
    re-anchor the cursor;
  - finally `renderCommits()`.
- `onerror`: `EventSource` auto-reconnects; nothing to do. No UI chrome is
  added (no "live" badge); the settings panel is the discoverability surface.
- The existing `window.focus` status refresh in `ops.js` stays (it covers the
  tab-in-background case when refresh is disabled).

## Error handling

- Watcher construction failure (fsnotify limits, missing dirs) → log to the
  op log via the existing failure ring if reachable, else silently fall back
  to interval polling for those sources (the TUI does the same: watch off ⇒
  interval backstop).
- `fetch`/`ls-remote` failures in the ticker are swallowed (background, no
  UI); `lastRun` is still stamped so a failing remote is not hammered.
- A subscriber that never reads is dropped after its buffer fills; the hub
  never blocks on a tab.

## Testing

- `live_test.go` (real git repos, `t.Parallel()` where no global state):
  - hub: `emit` reaches two subscribers; a full buffer drops without
    blocking; `stopLive` closes channels.
  - `TestEventsStreamsWatchChange`: start live with `branches_watch=true`,
    subscribe to `/api/events`, `git commit` in the repo, expect a message
    whose `changed` contains `branches` and `feed` within 3 s. Skipped when
    `gitwatch.Supported` is false (9p).
  - `TestEventsIntervalEmits`: `liveTick` and `now` are package vars
    overridable in tests; with `status=10`, advance the fake clock, expect
    `changed: [status]`.
  - `TestEventsSuppressedDuringOp`: with a parked op in flight, an emit is
    dropped.
  - `TestRerootRestartsLive`: after `/api/reroot` the hub's common dir is the
    new repo's.
- `settings_test.go`: `refresh_watch` round-trips to `<repo>/.gg.toml`
  (`branches_watch = true`), unknown keys 400, `watch_supported` present.
- Browser: the Playwright probe in the scratchpad — open the page, commit
  from a shell, assert the new subject appears without pressing `r`.
- Gates: `./test.sh && ./test.sh race`; the Windows suite notes stay valid
  (`gitwatch.Supported` is true off-Linux).

## Docs

CHANGELOG entry; `docs/web-tui-parity.md`: move "refresh settings" from
*Shared surface* to a truthful row and add live refresh as shared; README web
section: one sentence.
