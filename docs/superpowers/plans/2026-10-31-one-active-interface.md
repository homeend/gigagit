# One Active Interface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. This repo forbids implementer subagents: the session that wrote this plan executes it, task by task, in `.claude/worktrees/one-active-interface`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `gg` process shows either its TUI or its hosted web page; the other side shows a block with Activate and Quit.

**Architecture:** The TUI `Model` owns which side is active (`m.webActive`) and pushes a copy to the host (`WebHost.SetActive`). The server enforces the block with one wrapper around its handler and fans the flip out on `/api/events`. The CLI routes through a new `Inactive` flag on steer presence files.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla JS modules (no bundler), Playwright for the browser probe.

**Spec:** `docs/superpowers/specs/2026-10-31-one-active-interface-design.md`

## Global Constraints

- Exact labels: `Switch to web UI`, `Switch to TUI`, `Activate TUI`, `Activate web UI`, `Quit`, `Web UI is active`, `TUI is active`.
- Exact confirmations: `Switch to the web UI? The terminal stays blocked until you switch back.` / `Switch to the TUI? This page stays blocked until you switch back.`
- Exact refusals on `--to`: `the web UI is active in this gg` / `the terminal UI is active in this gg`.
- Every TUI string through `i18n.T` with literal keys in all four bundles; decision option values stay English.
- A static import of a non-exported name breaks the whole page and no Go test sees it: run the browser probe after JS edits, and `go test ./internal/web` after every `static/*.js` edit.
- Hideable web elements need their own `#id.hidden` rule.
- New tests call `t.Parallel()` unless they touch process-global state.

**One refinement of the spec:** §6 says the page reads the active flag from `/api/repo`. That handler runs git, so a blocked page would call git at boot. The plan adds `GET /api/ui-active` (no git) instead and leaves `/api/repo` off the allowlist.

## Review Focus

- **Browser never connects** (SSH, failed open): the terminal shows the URL, and Activate TUI works with zero tabs. Test in Task 2.
- **`--web-addr` port already taken at launch:** the TUI becomes active with the error on the status line, never stuck at `starting…`. Test in Task 3.
- **Two tabs, one presses Activate web UI:** both flip, neither reloads. Check in Task 4's probe.
- **`gg open --web <link>` with no tab open:** the new tab lands on the link, even though the command was posted before the tab connected. Test in Task 5.
- **While blocked, the page switches repository or starts an agent:** the terminal does not refuse, nothing draws over the block, and Activate TUI lands on the page's repository. Test in Task 3.

---

### Task 1: Server-side block

**Files:**
- Create: `internal/web/uiactive.go`, `internal/web/uiactive_test.go`
- Modify: `internal/web/server.go` (`Handler`, `Server` fields), `internal/web/host.go`, `internal/web/live.go` (`liveMsg`, hub), `internal/web/steer.go` (presence touch)

**Interfaces:**
- Produces, on `*web.Host`:
  - `SetActive(active bool)`: stores the flag, suspends or resumes the hub, re-touches `web.json`, fans out.
  - `SetActivator(fn func(ctx context.Context, side string) error)`: `side` is `"web"` or `"tui"`.
  - `Clients() int`: connected `/api/events` subscribers.
  - `Busy() string`: `"an operation is running"` when `opInFlight()`, else `""`.
- Produces, wire:
  - `GET /api/ui-active` → `{"active":bool,"hosted":bool}`, no git. A standalone server always says `active:true`.
  - `POST /api/ui-active` `{"side":"web"|"tui"}` (through `writeGuard`) → `{"ok":true}`, or `409` with the refusal reason.
  - Blocked refusal: `409` `{"error":"the terminal UI is active in this gg","ui_inactive":true}`.
  - `liveMsg{Reason:"ui_active", Active:*bool}`.
- Produces, `steer.Presence.Inactive bool` (`json:"inactive,omitempty"`), written on `web.json` while blocked.

- [ ] **Step 1: Write the failing tests** in `internal/web/uiactive_test.go`

  `TestUIGateAllowlist`, table over a hosted server with active false:
  - `GET /`, `GET /static/app.js`, `GET /api/ping`, `GET /api/events` (cancelled ctx), `GET /api/ui-active`, `GET /api/quit`, `POST /api/ui-active` are not `409`-with-`ui_inactive`.
  - `GET /api/repo`, `GET /api/status`, `POST /api/op`, `POST /api/steer`, `PUT /api/uistate` return `409` and `ui_inactive:true`.

  Also:
  - `TestUIGateStandaloneNeverBlocks`: `New(svc)`, `GET /api/ui-active` says active true, `GET /api/repo` is `200`.
  - `TestUIActiveFlipFansOut`: a subscriber gets `Reason=="ui_active"` with `*Active==true` after `SetActive(true)`.
  - `TestUIActivatePost`: the activator receives `"tui"` and its error becomes a `409` body. A nil activator is a `409`. `side:"x"` is a `400`.
  - `TestUIActiveHubSuspended`: while suspended, the tick lane does not run its sources; `SetActive(true)` resumes it.
  - `TestUIActivePresence`: `steer.Live(dir, steer.WebPresence)` reports `Inactive` true while blocked and false once active.

- [ ] **Step 2: Run them to verify they fail**

  Run: `go test ./internal/web -run 'TestUI' -count=1`. Expected: build failure on the missing names.

- [ ] **Step 3: Implement `uiactive.go`**

  - `func (s *Server) uiGate(next http.Handler) http.Handler`, wrapping the mux in `Handler()`. It passes everything when `!s.hosted || s.uiActive.Load()`.
  - The allowlist is matched on `r.URL.Path`.
  - `s.uiActive` is an `atomic.Bool`. A hosted server starts false; the TUI sets it.
  - Add `Inactive` to `steer.Presence` here, because Task 5 reads it.
  - The hub gains `setSuspended(bool)`, checked at the top of each `tickLoop` and `watchLoop` iteration. Drop while suspended, keep the goroutines and subscribers.

- [ ] **Step 4: Run the tests, then the package**

  Run: `go test ./internal/web -count=1`. Expected: PASS. Existing hosted tests that now hit the gate call `SetActive(true)` in their helper.

- [ ] **Step 5: Commit** `feat(web): a hosted page can be blocked behind the active interface`

---

### Task 2: TUI switch and blocked screen

**Files:**
- Create: `internal/tui/ui_switch.go`, `internal/tui/ui_switch_test.go`
- Modify: `internal/tui/webhost.go` (`WebHost` interface, `webHostState`, `startWebCmd`, `onWebStarted`; remove `openInBrowser`), `internal/tui/webhost_test.go` (`fakeWebHost`), `internal/tui/command_palette.go:74`, `internal/tui/command_palette_test.go:104`, `internal/tui/web_settings_popup.go`, `internal/tui/model.go` (`update`, `View`), `internal/tui/help.go`, the four i18n bundles

**Interfaces:**
- Consumes: Task 1's `SetActive`, `SetActivator`, `Clients`, `Busy`, added to the `WebHost` interface.
- Produces, in `ui_switch.go`:
  - `m.webActive bool`; `m.blockSel int`; `m.blockReason string`.
  - `func (m Model) askSwitchToWeb() (Model, tea.Cmd)`: decision `ID "ui-switch-web"`, options `"Switch"`, `"Cancel"`.
  - `func (m Model) uiSwitchRefusal() string`: `!opsIdle()` → `"an operation is running"`; `modal != nil` → `"a decision is waiting for the user"`; `proc != nil` → `"an interactive process owns the screen"`; else `""`.
  - `func (m Model) activateWeb() (Model, tea.Cmd)`: refusal check, then closes, adopts, starts or flips.
  - `func (m Model) activateTUI() (Model, tea.Cmd)`: `host.Busy()` refusal, then flips, reloads, resizes.
  - `func (m Model) closeForSwitch() Model`: `clearLayers`, plus `filesView`, `filesPreview`, `stashView`, `actionMenu`, `filterTyping`, `highlightTyping`, `recallOpen`.
  - `func (m Model) blockedMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool)`: the key and mouse gate.
  - `func (m Model) renderBlock() string`.
  - `webActivateRequestMsg{side string; reply chan error}` over `activatorFor` / `waitWebActivateCmd`, the `quitterFor` pattern.
  - `webStartedMsg` gains `activate bool`.

- [ ] **Step 1: Write the failing tests** in `ui_switch_test.go`, over `fakeWebHost` (extended with `active []bool`, `clients int`, `busy string`)

  - `TestSwitchToWebConfirms`: the palette row `Switch to web UI` sets `m.modal` with ID `ui-switch-web`; `Cancel` leaves `webActive` false.
  - `TestPaletteDropsOpenWebUI`: `paletteHas(m, "Open web UI in browser")` is false and `"Switch to web UI"` is true; absent when `NewWebHost == nil`.
  - `TestSettingsRowSwitches`: enter on `webRowSwitch` raises the same decision ID. Rename the `webRowOpen` constant.
  - `TestSwitchRefusedWhileBusy`: `m.running=true` keeps `webActive` false and the status line holds the reason.
  - `TestSwitchClosesWindows`: a layer plus `filterTyping` are gone after the flip.
  - `TestSwitchAdoptsLook`: viewing a worktree other than `m.home` moves `m.home` to it before the flip.
  - `TestSwitchOpensBrowserOnlyWithNoTab`: `clients=0` → `opens==1`; `clients=1` → `opens==0`.
  - `TestStartFailureStaysTUI`: `webStartedMsg{err}` leaves `webActive` false with the error on the status line.
  - `TestBlockedSwallowsKeys`: while blocked, `j`, `?`, `esc` change nothing.
  - `TestBlockedButtons`: `enter` on row 0 calls `activateTUI`; `down`, `enter` and `q` both yield a `tea.Quit` cmd; a mouse press on either row does the same. Row coordinates come from `blockRows`, the helper `renderBlock` uses.
  - `TestBlockedActivateRefused`: `busy="an operation is running"` stays blocked and `View()` contains the reason.
  - `TestBlockedViewShowsURL`: contains `Web UI is active`, the URL, `Activate TUI`, `Quit`; `starting…` before the URL is known; a 30-column width keeps every row on one line (`elidePath` on the URL).
  - `TestBlockedWithNoTabsCanActivate`: `clients=0`, `enter` activates the TUI.
  - `TestQuitHeldShowsOverBlock`: `quitHeldMsg` with live work shows the quit-mode popup in `View()`; `esc` returns to the block.
  - `TestActivatorFromPage`: a `webActivateRequestMsg{side:"web"}` flips and replies nil; with `proc != nil` it replies the refusal.
  - `TestActivateResendsConsoleSize`: activating with a shown console resizes the session to the panel's size.

- [ ] **Step 2: Run them to verify they fail**

  Run: `go test ./internal/tui -run 'TestSwitch|TestBlocked|TestPaletteDrops|TestSettingsRow|TestStartFailure|TestQuitHeld|TestActivat' -count=1`. Expected: build failure.

- [ ] **Step 3: Implement**

  Order inside `activateWeb`:
  1. refusal
  2. `closeForSwitch`
  3. adopt if viewed differs from `m.home`, via the path enter on a worktree row takes
  4. if no host, `startWebCmd(..., activate=true)` and show `starting…`
  5. `webActive=true`, `host.SetActive(true)`, `OpenBrowser` when `Clients()==0`

  Gate placement:
  - `blockedMsg` runs in `update` right after the `handoverDoneMsg` branch, and handles only `tea.KeyMsg` and `tea.MouseMsg`.
  - It lets `alt+U` through and routes to the top layer when the quit-mode sessions popup is open.
  - `View` branches to `renderBlock` after the `m.modal` check when `m.webActive` and no quit popup is up.

  The options gate needs `Switch` in `options_vocab_test` coverage. Add a `help.go` row under the existing web section.

- [ ] **Step 4: Run the tests, then the i18n gates**

  Run: `go test ./internal/tui -count=1`. Expected: PASS, including `i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`.

- [ ] **Step 5: Commit** `feat(tui): Switch to web UI and the blocked terminal screen`

---

### Task 3: Blocked terminal stays idle; `gg --web`

**Files:**
- Modify: `internal/tui/refresh.go:327` (`refreshSuppressed`), `internal/tui/model.go` (`watchEventMsg`, `memberWatchEventMsg`, `applyRepoHealth`), `internal/tui/task_track.go:78`, `internal/tui/webhost.go` (`startupWebCmd`), `internal/tui/run.go:86`, `internal/tui/websession.go`
- Test: `internal/tui/ui_switch_idle_test.go`

**Interfaces:**
- Consumes: Task 2's `m.webActive`, `activateTUI`.
- Produces:
  - `m.uiDirty bool`: a watch event arrived while blocked.
  - `m.deferredHealth *repoHealthMsg` (use the real message type): the launch health result held for first activation.
  - `webLaunchOptions.Web` now sets `m.webActive=true` at construction, and `startupWebCmd` passes `open=true` when `Web`.

- [ ] **Step 1: Write the failing tests**

  - `TestBlockedNoRefresh`: `refreshTick` while blocked returns no cmd and leaves `bgQueue` empty; `watchEventMsg` re-arms its wait, sets no `srcInflight`, sets `uiDirty`.
  - `TestActivationRefreshes`: `activateTUI` returns the full reload cmd and clears `uiDirty`.
  - `TestBlockedTaskResultWaits`: a task result arriving while blocked does not change the commit box and `tr.seen` is unchanged; after `activateTUI` it applies exactly once.
  - `TestWebLaunchBlockedFromFirstFrame`: `RunOptions{Web:true}`'s model has `webActive`, and `View()` shows `starting…`.
  - `TestWebLaunchPortTaken`: `webStartedMsg{err}` with `Web` → `webActive` false, error on the status line, `View()` no longer the block.
  - `TestWebServeLeavesTUIActive`: `[web] serve` with no `--web` → `webActive` false, host started, `SetActive(false)`, `opens==0`.
  - `TestLaunchPromptsWait`: repo health for a workspace-member repo while blocked raises no popup; first `activateTUI` raises the workspace question.
  - `TestPageSwitchWhileBlocked`: `onWebSwitchRequest` for another worktree while blocked replies nil and moves `m.home`; `View()` is still the block.
  - `TestPageStartsSessionWhileBlocked`: a `webSessionRequestMsg` while blocked starts the session and `View()` is still the block.

- [ ] **Step 2: Run them to verify they fail**

  Run: `go test ./internal/tui -run 'TestBlockedNo|TestActivationRefreshes|TestBlockedTask|TestWebLaunch|TestWebServe|TestLaunchPrompts|TestPageSwitch|TestPageStarts' -count=1`. Expected: FAIL.

- [ ] **Step 3: Implement**

  - `refreshSuppressed` returns true when `m.webActive`.
  - Watch events and `onTasksChanged` keep their re-arm, and skip their work while blocked. `onTasksChanged` is state-diff based through `tr.seen`, so calling it once from `activateTUI` delivers everything.
  - `activateTUI` batches the reload, `onTasksChanged` and any `deferredHealth`.
  - A session start while blocked must not call `openConsole`; it records the session only.

- [ ] **Step 4: Run the tests**

  Run: `go test ./internal/tui -count=1`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(tui): a blocked terminal stays idle; gg --web starts on the page`

---

### Task 4: The page

**Files:**
- Create: `internal/web/static/uiblock.js`, `internal/web/uiblockjs_test.go`
- Modify: `internal/web/static/index.html`, `style.css`, `app.js` (`boot`), `live.js`, `keys.js`, `layers.js`, `palette.js:85,558`, `ops.js` (`softSwitch`), `core.js`, `openfiles.js` (help text)

**Interfaces:**
- Consumes: Task 1's endpoints and `ui_active` message.
- Produces:
  - `state.uiBlocked` and `state.booted` in `core.js`.
  - In `uiblock.js`: `checkUIActive()`, `applyUIActive(active)`, `switchToTUI()`.
  - `closeAllLayers()` in `layers.js`.
  - `rereadWorktree(gen)` in `ops.js`: `softSwitch`'s read batch, called by `softSwitch` past its early return and by `applyUIActive`.
  - DOM: `#ui-block`, `#ui-block-activate`, `#ui-block-quit`, `#ui-block-err`, with `#ui-block.hidden`.

- [ ] **Step 1: Write the failing pins** in `uiblockjs_test.go`

  - `boot` calls `checkUIActive` before `preflightGate`.
  - `live.js` handles `ui_active` before any `changed` handling and drops other reasons while `state.uiBlocked`, except `shutdown`.
  - The palette and ☰ rows `Switch to TUI` are gated on `state.hosted`.
  - `keys.js` returns early on `state.uiBlocked`.
  - Exact confirmation text is present.

- [ ] **Step 2: Run them to verify they fail**

  Run: `go test ./internal/web -run TestUIBlockJS -count=1`. Expected: FAIL.

- [ ] **Step 3: Implement**

  - `applyUIActive(false)`: close windows, console and popups (`closeAllLayers`, viewer, compare, `leaveConsole`), then show the notice.
  - `applyUIActive(true)`: hide the notice, then `rereadWorktree` if `state.booted`, else continue `boot`.
  - A tab that boots blocked calls `connectLive` so it hears the flip, and `connectLive` must tolerate the later call in `boot`.
  - Activate web UI posts `POST /api/ui-active {side:"web"}` with no confirm. A refusal goes in `#ui-block-err`.
  - Quit reuses `quitFromMenu`. Its local confirm must render above `#ui-block`.
  - `launchTasks` does not run while blocked. The same applies to `takeTasks`; on activation, re-read `/api/tasks` through them so results apply once.
  - A `409` with `ui_inactive` from a request already in flight at the flip is swallowed in `getJSON` and `postJSON`.

- [ ] **Step 4: Run the package tests**

  Run: `go test ./internal/web -count=1`. Expected: PASS.

- [ ] **Step 5: Browser probe** (Firefox and Chromium; scratch `XDG_STATE_HOME`/`XDG_CONFIG_HOME`; fixture repo; a tmux-driven `bin/gg --web`)

  - With two tabs, the flip both ways shows `#ui-block` by geometry and `elementFromPoint` in both tabs.
  - `performance.getEntriesByType("navigation").length` stays 1 and no `beforeunload` fires.
  - A tab opened while blocked shows the notice, then boots on activate.
  - No `git` child spawns over 20 seconds while blocked (sample `pgrep -P <gg pid> git`).
  - Keys and clicks do nothing behind the notice.
  - The same run against the Task 3 build, unfixed, shows the pins fail for a real reason.

- [ ] **Step 6: Commit** `feat(web): the blocked page and Switch to TUI`

---

### Task 5: Agents and the CLI

**Files:**
- Modify: `internal/tui/steer.go:150,406`, `internal/tui/steer_kept.go:66`, `internal/tui/webhost.go` (`steerServe`), `internal/tui/session_snapshot.go`, `internal/cli/session.go:206,410,495`, `internal/cli/open.go`, `internal/web/host.go`, `internal/mcp/state.go:31`
- Test: `internal/cli/session_active_test.go`, `internal/cli/open_web_test.go`, `internal/tui/steer_test.go`

**Interfaces:**
- Consumes: `steer.Presence.Inactive`; `m.webActive`; `activateWeb`.
- Produces:
  - `sessionRoute` gains `tuiBlocked, webBlocked bool`. In `routeFor`: a blocked web page → `webOK=false, webBlocked=true`. A blocked TUI → `tuiOK=false, tuiBlocked=true`, and when `web` is not live and the presence carries a `URL`, `web=Presence{URL, Worktree}` and `webOK=true`.
  - `tui.json` while blocked: `Inactive:true, URL:<page>`, in `touchSteerPresence` and every kept inbox.
  - `sessionSnapshot.Interface string` (`json:"interface"`): `"tui"` or `"web"`, written at each flip.
  - `Host.SetStartAt(c steer.Command)`, to hand the link to a tab not yet connected (extend the standalone `startAt` path; verify its name in `web.Serve`).

- [ ] **Step 1: Write the failing tests**

  - `TestRouteBlockedTUIReachesPage`: `tui.json` with `Inactive` and a URL, no `web.json`; `sendSteer` POSTs to that URL (`httptest`).
  - `TestGGInboxAfterSwitchReachesPage`: `GG_INBOX` names such an inbox, `preferredInboxFor(dir,"web")` returns it.
  - `TestRouteBlockedPageReachesTUI`: `web.json` `Inactive`, live `tui.json`; goes to the inbox file with no HTTP.
  - `TestToBlockedSideRefused`: `--to tui` on a blocked TUI exits 1 with `the web UI is active in this gg`; `--to web` on a blocked page exits 1 with `the terminal UI is active in this gg`.
  - `TestStatusReportsBlocked`: `gg session status --json` has `tui_blocked:true`.
  - `TestOpenWebFlips`: a live TUI with a blocked page posts `serve`, not HTTP to the blocked page.
  - `TestOpenWebRefused`: `serve` answered with failure → exit 1 and the reason on stderr.
  - `TestOpenWebNoTabLands`: after `serve` with `Clients()==0`, the link is handed to `SetStartAt`, so the new tab's `applyStartAt` lands on it.
  - `TestOpenWithoutWebReachesPage`: `gg open <link>` with a blocked TUI posts to the page and does not flip.
  - `TestServeFlipsWithoutConfirm` (TUI): `applySteer(serve)` sets `webActive` with no modal; with `m.running` it answers failure.
  - `TestBlockedSteerRefused` (TUI): `navigate` while blocked answers `the web UI is active in this gg`; `reload` answers OK, starts no read and sets `uiDirty`.
  - `TestSnapshotNamesInterface`: the snapshot file holds `interface:"web"` after a flip.

- [ ] **Step 2: Run them to verify they fail**

  Run: `go test ./internal/cli ./internal/tui ./internal/mcp -run 'Route|GGInbox|ToBlocked|StatusReports|OpenWeb|OpenWithoutWeb|ServeFlips|BlockedSteer|SnapshotNames' -count=1`. Expected: FAIL.

- [ ] **Step 3: Implement**

  - `preferredInboxFor`'s `to != "web"` check on `TUIPresence` must also accept a TUI presence that is `Inactive` with a URL.
  - The `applySteer` block check goes right after the retired-verb check, and exempts `serve`, `reload` and `files`.
  - `steerServe` becomes: refusal → fail; else `activateWeb` and answer with the URL (through `pendingServe` when starting).
  - Update the `gg_ui_state` description: the session snapshot's `interface` says which side is active, and when it is `web` the snapshot describes a blocked terminal.

- [ ] **Step 4: Run the packages**

  Run: `go test ./internal/cli ./internal/tui ./internal/mcp ./internal/steer -count=1`. Expected: PASS.

- [ ] **Step 5: Commit** `feat: agents and gg open reach the active interface`

---

### Task 6: e2e, docs, gate

**Files:**
- Create: `e2e/scenarios/tui_web_active.toml` and its golden
- Modify: `internal/tui/headless.go` (seam to enter the blocked state), `README.md:1974`, `CHANGELOG.md`, `docs/web-tui-parity.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` (`tui`, `web` rows), `internal/agentskill/using-gg.md` (and `agentskill.Version`)

- [ ] **Step 1: Add the scenario** (read the `writing-e2e-scenarios` skill first)

  It opens the palette, types `web`, and golden-checks that the row reads `Switch to web UI`, then shows the blocked screen through the headless seam.

- [ ] **Step 2: Generate and read the goldens**

  Run: `go test ./e2e -run tui_web_active -update -count=1`, then read the golden by eye. Expected: the §1 screen.

- [ ] **Step 3: Update the docs** listed above, including the parity row and the `using-gg` lines for `gg open --web` and `gg session --to`.

- [ ] **Step 4: Run the staged suite**

  Run: `./test.sh`. Expected: `all green`.

- [ ] **Step 5: Commit** `docs: one active interface; e2e for the blocked terminal`

- [ ] **Step 6: Review on the most capable model** (read-only subagent, whole branch against the spec). Absorb the findings, commit.

- [ ] **Step 7: Sync and run the race gate**

  Merge `main` into the branch if it moved. Run `./test.sh race` with its log to a file; read the verdict in a separate call. Expected: the log says `all green`.

- [ ] **Step 8: Build** `bin/gg` in the worktree, give the user its absolute path, and ask before merging.
