# The TUI serves its own web page — design

Date: 2026-09-28. Status: agreed in brainstorming, ready for the plan.
Amends `2026-09-28-web-attach-design.md` (web attach): this is the stage
between its plan 1 (watch and type, merged `e1d839e5`) and its plan 2
(lifecycle).

## Goal

Agent sessions are process memory: a PTY plus an in-memory terminal
emulator held by `domain.Sessions()`, the one manager of the process that
started them. `gg web` was built as its own process (`cmd/gg/main.go`
routes `web` → `web.Serve`), so a session started in the TUI is invisible
to a page, and the console the web attach work built can only ever show
sessions of its own process.

This stage makes the TUI process serve the page. One process, one session
manager, both frontends reading from it. Everything the two frontends
shared before (git state, config, notes, saved comparisons, steering
presence) lives on disk and needed no such change; sessions are the first
shared state that does not.

## Rulings (agreed 2026-09-28 — do not re-ask)

1. **Turning it on: both.** A command "Open in browser" in the TUI (on
   demand: first use starts the server and opens the browser, later uses
   reopen the browser) AND an opt-in setting `[web] serve = true` that
   starts serving when the TUI launches (browser not opened). `gg web`
   stays as the standalone, no-TUI way.
2. **Address: random loopback port by default,** `[web] addr` to fix it,
   and a TUI launch flag `gg --web-addr <host:port>` (same syntax as
   `gg web --addr`). Flag beats config, config beats the default
   `127.0.0.1:0`. `gg --web` is the one-run equivalent of `serve = true`.
   The URL shows in the status line when serving starts and in the
   Settings popup; `gg session`/`gg open --web` find it through `web.json`.
3. **The page follows the TUI.** A TUI re-root (repo switcher, worktree
   switch, `gg open`, steer) re-roots the hosted server; open pages jump
   to the new repo.
4. **Lifecycle: the server dies with the TUI, no new prompt.** Re-root
   never stops it; only quit does. Open pages get the same "shutdown"
   message standalone `gg web` sends and paint their server-down bar.
5. **Fan out both change signals in domain:** the session list and each
   session's screen (and the task manager, same shape). Every frontend
   gets every wakeup; the web's one-second removal poll goes away.
6. **`gg open --web <link>` with a live TUI that is not serving:** steer
   the TUI to start serving (a new inbox command), wait for `web.json`,
   then post the link to that page as today. A TUI already hosting skips
   to the post. No live TUI → standalone `gg web`, unchanged.
7. **The hosted page hides its own repo switching** (sidebar repo rows,
   the palette's "open repo (path)…" entry, the worktree "switch here"
   row and the locks "go to worktree" arm). One process, one current
   repository, one switch point: the terminal. Standalone pages keep them.

## Architecture

```
 cmd/gg/main.go ── wires tui.NewWebHost = func(svc) tui.WebHost { return web.NewHost(svc, domain.OpenTUI) }
                   parses gg --web / --web-addr like --cwd-file; `gg web` unchanged
 internal/web   ── Serve (standalone: checks, then the core) · Host (the core: New, listen, live hub,
                   open-files watch, presence with URL, http.Server, graceful shutdown) · adoptService
                   · hosted flag on /api/repo · 409 on /api/reroot while hosted
 internal/tui   ── WebHost seam + webHostState (pointer field) · palette "Open in browser" · help row
                   · Settings "Web page" row · startup from [web] serve / --web · reRoot → host.Reroot
                   · Run tail → host.Close · inbox command "serve"
 internal/cli   ── gg open --web: live tui.json + no web.json → post "serve", await web.json, then post
 internal/agentsession ── broadcaster (Subscribe/cancel) replacing the one-slot Changed channels
 internal/domain ── TaskManager adopts the broadcaster; Sessions() unchanged otherwise
 internal/config ── [web] serve, addr + settingDocs
 internal/steer  ── Command.Cmd gains "serve"
```

Neither `internal/tui` nor `internal/web` imports the other (today's state,
kept; `internal/archtest` guards frontends only against store imports, the
seam keeps the two frontends apart by convention like `cli.LaunchWeb`).

## `internal/agentsession` — the broadcaster

```go
// broadcaster wakes every subscriber on Signal; bursts coalesce per subscriber.
type broadcaster struct { mu sync.Mutex; subs map[*subscriber]struct{} }
type subscriber struct { ch chan struct{} } // buffer 1

func (b *broadcaster) Subscribe() (<-chan struct{}, func())  // cancel drops the subscriber; idempotent
func (b *broadcaster) Signal()                               // non-blocking send to every subscriber
```

`Manager.Subscribe()` and `Session.Subscribe()` expose it; `Manager.Changed()`
and `Session.Changed()` are REMOVED (a future single reader would silently
reintroduce the theft). `domain.TaskManager` (`internal/domain/tasks.go`)
gets the same `Subscribe()` and loses `Changed()`.

Consumers converted:

| consumer | today | after |
|---|---|---|
| TUI `waitSessionsCmd` (console.go) | `Sessions().Changed()` per re-arm | one subscription on the Model's console pointer state, created at Init, re-armed per message, cancelled never (process lifetime) |
| TUI `waitSessionCmd` (console.go) | `s.Changed()` per re-arm | a subscription per open console held on `consoleState`, cancelled when the console closes or its session is removed |
| TUI `waitTasksCmd` (task_track.go) | `Tasks().Changed()` | one subscription on `taskTrack` |
| web `watchSessions` (sessions_http.go) | the ONLY web receiver | subscribes; cancels when `sessStop` closes |
| web producer `produce` (console_stream.go) | `sess.Changed()` + 1 s removal poll | subscribes to the session AND the manager; on a manager signal re-checks `Sessions().Get(id)` → `gone`; both cancelled when the producer ends |

Subscriptions are values on pointer fields, never re-created per message,
so a burst cannot be lost between two arms and no goroutine outlives its
owner.

**Order of work:** `web.New` subscribes to the manager in its constructor.
The broadcaster is task 1 of the plan; a host inside the TUI process may
not exist before it.

## `internal/web` — Serve split, Host, hosted rules

### `Serve` (standalone, unchanged behaviour)

Keeps: `domain.Open`, `preflight`, `PreflightRequired`, `RunAutoMigrations`,
`touchMRU`, `applyUIPolicies`, `setStartAt`, the interrupt context, the
browser open, the stderr "gg web: serving <url>" line. Then runs the core.

### `Host` (the core)

```go
// Host is one served page over a caller-owned Service. Serve wraps it for
// the standalone command; the TUI hosts it in-process.
type Host struct { srv *Server; httpSrv *http.Server; url string; ... }

// NewHost builds the server over svc. opener is how the hosted server
// opens a Service for a path of its own (the TUI passes domain.OpenTUI so
// an ssh prompt can never reach the raw-mode terminal; Serve passes
// domain.Open). hosted marks the page as terminal-owned (ruling 7).
func NewHost(svc *domain.Service, opener func(string) *domain.Service, hosted bool) *Host
func (h *Host) Start(ctx context.Context, addr string) (url string, err error)
func (h *Host) Reroot(ctx context.Context, svc *domain.Service) error
func (h *Host) URL() string
func (h *Host) Close()   // announceShutdown → http Shutdown(shutdownGrace) → srv.Close → removeSteerPresence
```

`Start`: refuse when a foreign `web.json` is live in the resolved steer dir
(`steer.Live(dir, steer.WebPresence)` with a PID other than ours) — error
`another gg web page is already serving this worktree at <url>`; then
`listen(addr)`, `startLive`, `startOpenFilesWatch`, `initSteerPresence(url)`,
`http.Server{Handler: srv.Handler()}` served on a goroutine. The hosted
server does NOT re-apply `applyUIPolicies` at start: the TUI pushes the
same policies onto the shared Service itself (`load.go`), and its
`[versions]` handling is the source of truth. (The web settings write path
still re-applies after ITS writes — same config, harmless.)

`Reroot(svc)`: `adoptService(ctx, svc)` — the post-swap tail lifted out of
`handleReroot`: store the Service under `opMu` (409 `errOpBusy` if an op is
live: the TUI's switch guard makes this unreachable in practice, but the
lock discipline stays), drop `s.cur` and `s.feed`, `restartLive`,
`rehomeSteerPresence`, `touchMRU`. `handleReroot` keeps its allowlist,
custom-path, repair and preflight lanes and ends in `adoptService`; while
hosted it answers 409 `"the terminal owns the current repository"` before
any of that. Its `domain.Open(target)` becomes `s.opener(target)`.

### Wire changes

- `GET /api/repo` gains `"hosted": true|false`.
- `POST /api/reroot` → 409 while hosted.
- The page (`static/`): `core.js` state gains `hosted` from `/api/repo`;
  `sidebar.js` skips the repo rows and the worktree "switch here" row,
  `palette.js` skips "open repo (path)…", `locks.js` skips "go to
  worktree" when `hosted`. Everything else (worktree files, sessions,
  consoles, ops) is unchanged.

## `internal/tui` — surface and lifecycle

### Seam and state

```go
// WebHost is the page the TUI can serve from its own process; cmd/gg sets
// NewWebHost. nil = unavailable (the palette entry is absent).
type WebHost interface {
    Start(ctx context.Context, addr string) (string, error)
    Reroot(ctx context.Context, svc *domain.Service) error
    URL() string
    Close()
}
var NewWebHost func(svc *domain.Service) WebHost

type webHostState struct { host WebHost; url string; starting bool; err string }  // Model.web *webHostState
```

### Commands

- Palette `Open in browser` (alphabetical, between "Open gg:// link…" and
  "Open repo"), present only when `NewWebHost != nil`. Not serving →
  `startWebCmd(addr)` (async: `NewWebHost(m.svc).Start`) → `webStartedMsg{url, err}`;
  on success status `web page: serving <url>` and `openBrowser(url)` (the
  TUI reuses the web package's opener through the seam: `WebHost` gains
  `OpenBrowser()`; no second copy of the `$BROWSER`/WSL logic). Serving →
  `OpenBrowser()` only. Starting → status "web page: starting…".
- `?` help: a row for the palette entry (`TestHelpFooterCoverage`).
- Settings popup: row `Web page` → `Web page: <url>` or `Web page: not
  running`; its section has `Serve at startup: on/off` (`[web] serve`) and
  `Address: <addr or "random port">` (`[web] addr`, a text field) with the
  usual global/repo scope, plus `Open in browser` as an action row.
- Inbox command `serve` (`applySteer`): start if not serving; reply
  `OK` with the URL in `Reply.Detail` either way, or `Reply.Error`.
- No dedicated key (single letters are exhausted; `W` is taken).

### Startup

`Run`: after `m.steerDir` resolves (so `web.json` lands in the right
inbox), when `cfg.Web.Serve || opts.Web`, `Init` includes
`startWebCmd(addr)` with `addr = opts.WebAddr` else `cfg.Web.Addr` else "".
`Run` gains a `RunOptions{Web bool; WebAddr string}` parameter (the
existing `recordPath`, `at` fold into it — one struct, one call site).

### Re-root and quit

- `reRoot`: right after `m.svc = domain.OpenTUI(path)`: `if m.web != nil && m.web.host != nil { cmds = append(cmds, rerootWebCmd(m.web.host, m.svc)) }`
  (async; a failure lands in the status line, the host keeps the old repo).
- `Run` tail: after `domain.Sessions().KillAll`, `fm.web.host.Close()`.

## `internal/cli` — `gg open --web`

`openWeb` and `openWebBare`, before starting a standalone server: if
`steer.Live(dir, steer.TUIPresence)` and no live `web.json`, `steer.Post`
`{Cmd: "serve"}` and `AwaitReply` (5 s); on `ok`, re-read `web.json` and
proceed with the HTTP post (or print the URL for the bare form). A timeout
or an error reply prints it and exits 1 (never starts a second process
beside a live TUI). `gg session status` needs no change (a hosted page is a
page).

## `internal/config` — `[web]`

```go
type WebConfig struct {
    Serve bool   `toml:"serve"` // serve the web page when the TUI starts (default false; a bool is fine: off is the default and the zero value)
    Addr  string `toml:"addr"`  // listen address for the hosted page; empty = 127.0.0.1:0 (random port); loopback only
}
```

`settingDocs` rows for both (`template_test.go` enforces); `write.go` gains
the two scoped setters the Settings popup uses (per `adding-config-entries`).

## `cmd/gg/main.go`

- `extractWebFlags(args) (web bool, addr string, rest []string)` next to
  `extractCwdFile` (`--web`, `--web-addr X`, `--web-addr=X`); passed to
  `launchTUI` → `tui.Run(svc, tui.RunOptions{...})`.
- `tui.NewWebHost = func(svc) tui.WebHost { return web.NewHost(svc, domain.OpenTUI, true) }`
  set beside `cli.LaunchWeb`.
- `runWeb`/`gg web --addr/--open` unchanged.

## Error handling

| case | behaviour |
|---|---|
| bind fails (port taken, bad `[web] addr`) | status line `web page: <err>`; no host kept; the palette entry retries |
| non-loopback `[web] addr` | the existing `listen` refusal, shown in the status line |
| foreign live `web.json` | refuse with the other URL in the status line (never overwrite a standalone page's presence) |
| re-root fails on the host | status line; the host keeps serving the old repo; the TUI's switch is unaffected |
| `serve` inbox command while not hostable (`NewWebHost == nil`) | reply error `this gg cannot serve a web page` |
| `gg open --web` `serve` timeout | print `the TUI in <dir> did not start its web page`, exit 1 |
| page calls `/api/reroot` while hosted | 409 with the terminal-owns message (the buttons are hidden; this is the belt) |

## Testing

- **agentsession/domain:** two subscribers both wake on one `Signal`; a
  burst before either reads → one wakeup each; cancel drops a subscriber,
  blocks nothing, receives nothing; `Manager.Start`/exit and
  `Session` output drive it through the real paths; `TaskManager` likewise.
- **web:** `TestHostStartServesHosted` (temp repo → `Start("127.0.0.1:0")`,
  loopback URL, `/api/repo` `hosted:true`, `web.json` holds the URL);
  `TestHostRerootFollows` (add a worktree, `Reroot(domain.Open(wt))` →
  `/api/repo` worktree changes, `web.json` moved); `TestHostRefusesForeignPage`;
  `TestHostRerootEndpoint409WhenHosted`; `TestHostCloseAnnouncesShutdown`
  (an `/api/events` stream reads `shutdown`, presence gone);
  `TestServeStandaloneNotHosted`; `TestHandleRerootUsesOpener`; producer
  `gone` now arrives via the manager signal (existing test, no poll);
  JS: `hosted` hides the four switch affordances (the existing `*js_test.go`
  wiring style).
- **tui:** a recording fake `WebHost`: palette entry starts once then
  reopens; `reRoot` → `Reroot` with the new `*domain.Service`; `Run` tail
  → `Close` (via the model's close path, tested without a program);
  `RunOptions.Web`/`cfg.Web.Serve` start at Init; `--web-addr` beats
  `[web] addr`; Settings row text; `serve` inbox command → started + reply;
  help/footer coverage; i18n bundles.
- **cli:** `gg open --web` with live `tui.json`, no `web.json`: posts
  `serve`, then posts the link to the URL the reply/presence gives; the
  timeout arm.
- **cmd/gg:** `extractWebFlags` shapes.
- **Browser check (assert visibility; unfixed install first).** Unfixed:
  `gg --web` → `gg: unknown command` / rejected; a `tui-capture.sh` run
  shows no "Open in browser" in ctrl+p. Fixed: `tui-capture.sh`-style
  tmux session of the built `gg --web --web-addr 127.0.0.1:0` in a temp
  repo with isolated `XDG_STATE_HOME`/`XDG_CONFIG_HOME`; start a terminal
  session from the Worktrees `.` menu ("Open terminal in <wt>"); read the
  URL from `web.json` under the isolated state dir (never from a log);
  playwright: ctrl+\ → the Agents tab lists the session (visible), enter →
  the console shows the shell prompt (visible), `/api/repo` says hosted
  and the sidebar has no repo rows; `q` + confirm kill in the TUI → the
  page paints its server-down bar (visible). Kill only our own tmux
  session and PID.
- Gates: `go test ./internal/web/ ./internal/tui/` at every task;
  `./test.sh` and `./test.sh race` before merge.

## Docs

CHANGELOG (always); README: the palette command, `[web]`, `gg --web`,
`--web-addr`, `gg open --web` with a live TUI; CLAUDE.md map rows `web`
(hosted in-process by the TUI) and `tui` (serves the page), one line each;
`docs/CLAUDE-details.md`: host lifecycle, the broadcaster and its
subscription-lifetime rule, the `serve` inbox command, the hosted-page
rules; the settings registry (`config-settings-registry` memory); memory
`agent-sessions-feature.md`.

## Out of scope

Plan 2 (start dialog, kill/remove from the page, Branches-row parity) and
plan 3 (session states) of the web attach spec; a page-side switch that
re-roots the TUI; a server outliving the TUI; a dedicated TUI key for the
page; changes to `gg web`'s own flags.
