# The TUI serves its own web page — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo never uses subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The TUI process hosts the gg web page, so a browser tab and the terminal share one agent-session manager; both frontends receive every session change.

**Architecture:** A broadcaster in `agentsession` replaces the one-slot `Changed` channels (per-subscriber coalescing). `web.Serve` splits into the standalone entry and a reusable `Host` the TUI drives through a `tui.WebHost` seam wired in `cmd/gg/main.go` (the `cli.LaunchWeb` pattern). The TUI's re-root hands its new Service to the host; the hosted page hides its own repo switching; `gg open --web` asks a live TUI to serve through a new `serve` inbox command.

**Tech Stack:** Go 1.26, Bubble Tea v1, net/http + SSE, the embedded SPA (vanilla JS modules), TOML config, tmux + playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-09-28-web-hosted-in-tui-design.md` (amends `2026-09-28-web-attach-design.md`).

## Global Constraints

- NEVER use subagents; every task runs in this session (CLAUDE.md).
- Work in the worktree `.claude/worktrees/web-hosted` on `feat/web-hosted`; commit per task; never touch the main checkout.
- TDD: write the failing test, watch it fail, then implement. `go test ./internal/web/ ./internal/tui/` after every task; `./test.sh` and `./test.sh race` before merge; never edit files while `./test.sh` runs.
- `internal/tui` and `internal/web` never import each other; `cmd/gg` is the composition root.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in `internal/i18n/lang/{ja,ko,zh,ru}.toml` (AST gates).
- No plan numbers or internal names in user-facing text. Key hints only in the footer/`#foot`. The web hides by id (`#id.hidden`).
- Loopback only: `listen` refuses non-loopback hosts (unchanged).
- The broadcaster lands before any host exists in the TUI process (task order 1 → 3 before 6/7).
- `Changed()` is removed from `Manager`, `Session` and `TaskManager` at the end of task 3, so no single-reader channel survives.

## Review Focus

1. A console open on a session whose process exits while the browser also watches it: both must show the exit (terminal status line, page `exited (N)`), and the terminal's console must still repaint the last screen (task 3's per-console subscription test).
2. Two TUI re-roots in quick succession while a page is open: the page must end on the LAST repo and `web.json` must exist only in that repo's inbox (task 6 `TestHostRerootFollows` covers one; add the double re-root case there).
3. Quit while a page holds an open `/api/session-screen` stream: the process must exit within `shutdownGrace` and the page must show the server-down bar (task 6 `TestHostCloseAnnouncesShutdown` with a screen stream attached).
4. `gg open --web` with a stale `tui.json` (a TUI that crashed under 5 s ago): the `serve` post gets no reply; the CLI must print the timeout message and exit 1 rather than start a second server beside a possibly-live TUI (task 9 timeout test).
5. `[web] addr` pointing at a port already in use: the TUI must stay usable and say so in the status line; a second "Open in browser" retries (task 7 bind-failure test through the fake host).

---

### Task 1: The broadcaster — `Subscribe()` on Manager, Session and TaskManager

**Files:**
- Create: `internal/agentsession/broadcast.go`, `internal/agentsession/broadcast_test.go`
- Modify: `internal/agentsession/manager.go:20-40`, `internal/agentsession/session.go:97-99,269-281`, `internal/domain/tasks.go:86-121`
- Test: `internal/agentsession/manager_test.go`, `internal/domain/tasks_test.go`

**Interfaces:**
- Produces: `func (m *Manager) Subscribe() (<-chan struct{}, func())`, `func (s *Session) Subscribe() (<-chan struct{}, func())`, `func (m *TaskManager) Subscribe() (<-chan struct{}, func())`. `Changed()` stays on all three until task 3 removes it.

- [ ] **Step 1: Write the failing broadcaster test**

```go
// internal/agentsession/broadcast_test.go
package agentsession

import (
	"testing"
	"time"
)

func recv(t *testing.T, ch <-chan struct{}) bool {
	t.Helper()
	select {
	case <-ch:
		return true
	case <-time.After(200 * time.Millisecond):
		return false
	}
}

func TestBroadcasterWakesEverySubscriber(t *testing.T) {
	t.Parallel()
	var b Broadcaster
	a, cancelA := b.Subscribe()
	defer cancelA()
	c, cancelC := b.Subscribe()
	defer cancelC()
	b.Signal()
	if !recv(t, a) || !recv(t, c) {
		t.Fatal("both subscribers must wake on one Signal")
	}
}

func TestBroadcasterCoalescesPerSubscriber(t *testing.T) {
	t.Parallel()
	var b Broadcaster
	a, cancel := b.Subscribe()
	defer cancel()
	b.Signal()
	b.Signal()
	b.Signal()
	if !recv(t, a) {
		t.Fatal("first wakeup")
	}
	if recv(t, a) {
		t.Fatal("a burst before the read must coalesce into ONE wakeup")
	}
}

func TestBroadcasterCancelDropsTheSubscriber(t *testing.T) {
	t.Parallel()
	var b Broadcaster
	a, cancel := b.Subscribe()
	cancel()
	cancel() // idempotent
	b.Signal()
	if recv(t, a) {
		t.Fatal("a cancelled subscriber receives nothing")
	}
	if n := b.count(); n != 0 {
		t.Fatalf("subscribers after cancel = %d, want 0", n)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd .claude/worktrees/web-hosted && go test ./internal/agentsession/ -run TestBroadcaster -v 2>&1 | tail -5`
Expected: FAIL to compile, `undefined: Broadcaster`.

- [ ] **Step 3: Implement the broadcaster**

```go
// internal/agentsession/broadcast.go
package agentsession

import "sync"

// Broadcaster wakes every subscriber on Signal (exported: domain.TaskManager
// embeds one). Each subscriber owns a
// one-slot channel, so a burst coalesces per subscriber instead of being
// stolen by whichever reader is fastest — the shape a TUI and a web page
// in ONE process need. The zero value is ready to use.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// Subscribe returns a channel that receives after every Signal since the
// last receive, and a cancel that drops the subscription (idempotent).
func (b *Broadcaster) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	if b.subs == nil {
		b.subs = map[chan struct{}]struct{}{}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Signal marks every subscriber dirty without ever blocking the caller.
func (b *Broadcaster) Signal() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// count is a test hook.
func (b *Broadcaster) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
```

- [ ] **Step 4: Run the broadcaster tests**

Run: `go test ./internal/agentsession/ -run TestBroadcaster -v 2>&1 | tail -5`
Expected: PASS ×3.

- [ ] **Step 5: Write the failing Manager/Session/TaskManager subscription tests**

Append to `internal/agentsession/manager_test.go`:

```go
func TestManagerSubscribeSeesStartOnTwoSubscribers(t *testing.T) {
	m := NewManager()
	a, cancelA := m.Subscribe()
	defer cancelA()
	b, cancelB := m.Subscribe()
	defer cancelB()
	s, err := m.Start(StartSpec{Label: "t", Argv: []string{"sh", "-c", "exit 0"}, Cols: 20, Rows: 5, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.KillAll(t.Context()) })
	if !recv(t, a) || !recv(t, b) {
		t.Fatal("both subscribers must see the start")
	}
	<-s.Done()
}

func TestSessionSubscribeSeesOutputOnTwoSubscribers(t *testing.T) {
	m := NewManager()
	s, err := m.Start(StartSpec{Label: "t", Argv: []string{"sh", "-c", "sleep 0.1; echo hi"}, Cols: 20, Rows: 5, Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.KillAll(t.Context()) })
	a, cancelA := s.Subscribe()
	defer cancelA()
	b, cancelB := s.Subscribe()
	defer cancelB()
	if !recv(t, a) || !recv(t, b) {
		t.Fatal("both subscribers must see the screen change")
	}
}
```

(Skip both on Windows the way `manager_test.go` already skips sh-based tests.) Append to `internal/domain/tasks_test.go`, next to the existing `Changed()` test at line 246, a copy of that test that calls `m.Subscribe()` twice and asserts both channels receive.

- [ ] **Step 6: Run them to verify they fail**

Run: `go test ./internal/agentsession/ ./internal/domain/ -run 'Subscribe' 2>&1 | tail -5`
Expected: compile errors `m.Subscribe undefined`, `s.Subscribe undefined`.

- [ ] **Step 7: Wire the broadcaster into the three owners**

`internal/agentsession/manager.go`: replace the `changed chan struct{}` field with `bc Broadcaster`; `NewManager` drops the channel make; `signal()` becomes `m.bc.Signal()`; keep `Changed()` for now as:

```go
// Changed is the single-reader form kept until every consumer subscribes
// (removed in the same change that converts the TUI). Deprecated.
func (m *Manager) Changed() <-chan struct{} { ch, _ := m.bc.Subscribe(); return ch }

// Subscribe wakes the returned channel on every list change (start, exit,
// remove); bursts coalesce per subscriber. cancel drops the subscription.
func (m *Manager) Subscribe() (<-chan struct{}, func()) { return m.bc.Subscribe() }
```

`internal/agentsession/session.go`: same swap (`changed` field → `bc Broadcaster`, drop the make at line 99, `signal()` → `s.bc.Signal()`, `Changed()` as above, add `Subscribe()`).

`internal/domain/tasks.go`: same swap on `TaskManager` (`changed` → `bc agentsession.Broadcaster`; `Subscribe`/`Signal` are exported, `count` stays a package-local test hook).

- [ ] **Step 8: Run the package tests**

Run: `go build ./... && go test ./internal/agentsession/ ./internal/domain/ 2>&1 | tail -5`
Expected: PASS (the old `Changed()` tests still pass through the compatibility shim).

- [ ] **Step 9: Commit**

```bash
git add internal/agentsession/broadcast.go internal/agentsession/broadcast_test.go internal/agentsession/manager.go internal/agentsession/manager_test.go internal/agentsession/session.go internal/domain/tasks.go internal/domain/tasks_test.go
git commit -m "feat(agentsession): a Broadcaster behind Manager, Session and TaskManager — every subscriber wakes on every change"
```

---

### Task 2: The web subscribes — list watcher and screen producer

**Files:**
- Modify: `internal/web/sessions_http.go:106-119`, `internal/web/console_stream.go:30-36,181-218`
- Test: `internal/web/console_stream_test.go`, `internal/web/sessions_http_test.go`

**Interfaces:**
- Consumes: `domain.Sessions().Subscribe()`, `(*domain.AgentSession).Subscribe()` (task 1).
- Produces: nothing new on the wire; `feedGonePoll` is deleted.

- [ ] **Step 1: Write the failing test — a removal reaches the stream at once**

Append to `internal/web/console_stream_test.go` (reuse its helpers: the fake manager via `domain.UseSessionManager`, `newRepoDir`, `serve`, the SSE reader that stops at `gone`):

```go
func TestFeedGoneArrivesWithoutPolling(t *testing.T) {
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(restore)
	srv := New(domain.Open(newRepoDir(t, 1)))
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	s, err := domain.Sessions().Start(domain.SessionStartSpec{Label: "sh", AgentID: "sh", Repo: "r", Dir: t.TempDir(), Cols: 20, Rows: 5, Argv: []string{"sh", "-c", "exit 0"}})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	events := openScreenStream(t, ts.URL, s.Info().ID) // the helper the gone test already uses
	start := time.Now()
	if err := domain.Sessions().Remove(s.Info().ID); err != nil {
		t.Fatal(err)
	}
	waitForEvent(t, events, "gone", 500*time.Millisecond)
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("gone took %v; it must ride the manager signal, not a 1 s poll", time.Since(start))
	}
}
```

Adapt the two helper names to what `console_stream_test.go` actually defines (read the file first; the gone test at the bottom has the reader).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestFeedGoneArrivesWithoutPolling -v 2>&1 | tail -5`
Expected: FAIL — `gone took 1.0…s`.

- [ ] **Step 3: Convert the producer and the list watcher**

`console_stream.go`: delete `feedGonePoll` and its comment; in `produce`:

```go
func (f *screenFeeds) produce(sess *domain.AgentSession, stop <-chan struct{}) {
	id := sess.Info().ID
	screen, cancelScreen := sess.Subscribe()
	defer cancelScreen()
	list, cancelList := domain.Sessions().Subscribe()
	defer cancelList()
	var timer *time.Timer
	var fire <-chan time.Time
	exited := sess.Info().State == domain.SessionExited
	for {
		select {
		case <-stop:
			return
		case <-screen:
			if timer == nil {
				timer = time.NewTimer(feedCoalesce)
				fire = timer.C
			}
		case <-fire:
			timer, fire = nil, nil
			f.publish(id, sess.ScreenRuns())
			if info := sess.Info(); info.State == domain.SessionExited && !exited {
				exited = true
				code := info.ExitCode
				f.send(id, feedMsg{Exited: &code})
			}
		case <-list:
			if _, ok := domain.Sessions().Get(id); !ok {
				f.send(id, feedMsg{Gone: true})
				return
			}
		}
	}
}
```

`sessions_http.go` `watchSessions`:

```go
// watchSessions forwards the manager's change signal to the tabs until stop
// closes. Started by New, stopped by Close. Its own subscription: the TUI
// in the same process has one too, and neither steals the other's wakeups.
func (s *Server) watchSessions(stop <-chan struct{}) {
	ch, cancel := domain.Sessions().Subscribe()
	defer cancel()
	for {
		select {
		case <-stop:
			return
		case <-ch:
			s.broadcastSessions()
		}
	}
}
```

Update the comment on `handleSessionScreen`/`attach` that mentions polling. Also fix the sentence in `docs/CLAUDE-details.md` "removal POLLED" in the web attach section (one line).

- [ ] **Step 4: Run the web tests**

Run: `go test ./internal/web/ 2>&1 | tail -5`
Expected: PASS, including the new test and the existing gone/exited/slow-stream tests.

- [ ] **Step 5: Commit**

```bash
git add internal/web/console_stream.go internal/web/console_stream_test.go internal/web/sessions_http.go docs/CLAUDE-details.md
git commit -m "feat(web): session streams subscribe to the broadcaster — removal arrives on the signal, no poll"
```

---

### Task 3: The TUI subscribes; `Changed()` is removed everywhere

**Files:**
- Modify: `internal/tui/console.go:21-27,54-63,115-131,185-220`, `internal/tui/task_track.go:14-52`, `internal/tui/model.go:477,570-580`, `internal/agentsession/manager.go`, `internal/agentsession/session.go`, `internal/domain/tasks.go`, `internal/agentsession/manager_test.go:100`, `internal/agentsession/session_test.go:92`, `internal/domain/tasks_test.go:246`
- Test: `internal/tui/console_test.go` (existing file; add cases)

**Interfaces:**
- Consumes: the three `Subscribe()`s.
- Produces: `Model.sessWatch *sessionWatch` (pointer field; list subscription for the process lifetime), `consoleState.screen <-chan struct{}` + `consoleState.cancel func()`, `taskTrack.ch <-chan struct{}`.

- [ ] **Step 1: Write the failing test — two consoles in a row do not leak subscriptions, and a closed console cancels**

Append to `internal/tui/console_test.go` (use its existing session-start helper; it runs real `sleep` sessions, serial):

```go
func TestConsoleCloseCancelsItsScreenSubscription(t *testing.T) {
	m, id := modelWithRunningSession(t) // the file's existing helper that starts a sleep session and returns the loaded model + id
	m, _ = m.openConsole(id)
	if m.console == nil || m.console.cancel == nil {
		t.Fatal("an open console holds a screen subscription")
	}
	s, _ := domain.Sessions().Get(id)
	before := s.SubscriberCount()
	m = m.closeConsole()
	if after := s.SubscriberCount(); after != before-1 {
		t.Fatalf("subscribers after close = %d, want %d", after, before-1)
	}
}
```

Add `func (s *Session) SubscriberCount() int { return s.bc.count() }` on `agentsession.Session` (a test hook; documented as such) and the domain alias if `AgentSession` is a type alias (it is — `domain.AgentSession = agentsession.Session`, check `internal/domain/sessions.go`).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestConsoleCloseCancelsItsScreenSubscription -v 2>&1 | tail -5`
Expected: FAIL to compile, `m.console.cancel undefined`.

- [ ] **Step 3: Convert the TUI**

`console.go`:

```go
type consoleState struct {
	id        domain.SessionID
	focused   bool
	maximized bool
	gen       int
	highHalf  rune
	screen    <-chan struct{} // this console's subscription to its session's screen
	cancel    func()          // drops it; called by every path that clears m.console
}

// sessionWatch is the TUI's process-lifetime subscription to the session
// list (a pointer field: the value-receiver Model must share it).
type sessionWatch struct{ ch <-chan struct{}; cancel func() }

func newSessionWatch() *sessionWatch {
	ch, cancel := domain.Sessions().Subscribe()
	return &sessionWatch{ch: ch, cancel: cancel}
}

func waitSessionCmd(c *consoleState, id domain.SessionID, gen int) tea.Cmd {
	ch := c.screen
	return func() tea.Msg {
		<-ch
		time.Sleep(consoleRepaint)
		return consoleChangedMsg{id: id, gen: gen}
	}
}

func (m Model) waitSessionsCmd() tea.Cmd {
	ch := m.sessWatch.ch
	return func() tea.Msg { <-ch; return sessionsChangedMsg{} }
}
```

In `openConsole`: `m = m.dropConsole()` before building the new state (cancels a previous console's subscription), then `screen, cancel := s.Subscribe()` and `m.console = &consoleState{id: id, focused: true, gen: gen, screen: screen, cancel: cancel}`; return `waitSessionCmd(m.console, id, gen)`. Add:

```go
// dropConsole clears the console and its subscription. Every place that
// sets m.console = nil goes through it (closeConsole, onSessionsChanged,
// file_preview.go:166, stash_view.go:332).
func (m Model) dropConsole() Model {
	if m.console != nil && m.console.cancel != nil {
		m.console.cancel()
	}
	m.console = nil
	return m
}
```

`closeConsole` → `m = m.dropConsole()`; `onSessionsChanged` line 214 → `m = m.dropConsole()`; `file_preview.go:166` and `stash_view.go:332` → `m = m.dropConsole()`; `onSessionsChanged` returns `m.waitSessionsCmd()`. `model.go:576` → `waitSessionCmd(m.console, msg.id, msg.gen)` after the existing gen check. `New(svc)` sets `sessWatch: newSessionWatch()`; `Init` uses `m.waitSessionsCmd()`. Guard: `waitSessionsCmd` on a literal Model with nil `sessWatch` returns `nil` (tests build literals).

`task_track.go`: `taskTrack` gains `ch <-chan struct{}`; `newTaskTrack` calls `domain.Tasks().Subscribe()` (cancel kept in the struct, unused: process lifetime); `waitTasksCmd` becomes `(m Model) waitTasksCmd()` reading `m.taskTrack.ch` after `ensureTaskTrack`; `Init` and `onTasksChanged` use it.

Then remove `Changed()` from `Manager`, `Session`, `TaskManager`, and switch the three tests at `manager_test.go:100`, `session_test.go:92`, `tasks_test.go:246` to `Subscribe()`.

- [ ] **Step 4: Build everything and run the three packages**

Run: `go build ./... && go vet ./internal/agentsession/ ./internal/domain/ ./internal/tui/ ./internal/web/ && go test ./internal/agentsession/ ./internal/domain/ ./internal/tui/ ./internal/web/ 2>&1 | tail -8`
Expected: PASS; `grep -rn "\.Changed()" internal/ cmd/` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add -A internal/tui internal/agentsession internal/domain
git commit -m "feat(tui): consoles, the session list and the task tracker subscribe to the broadcaster; Changed() is gone"
```

---

### Task 4: `[web] serve` and `[web] addr`

**Files:**
- Modify: `internal/config/config.go` (struct at ~211-227, `Defaults()`, `Load` overlay calls at ~267), `internal/config/template.go:26-90`, `internal/config/write.go`
- Test: `internal/config/config_test.go`, `internal/config/write_test.go`

**Interfaces:**
- Produces: `config.WebConfig{Serve bool; Addr string}` as `Config.Web`; `config.SetWebServe(path string, on bool) error`, `config.SetWebAddr(path, addr string) error`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/config/config_test.go
func TestWebLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")
	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Web.Serve || cfg.Web.Addr != "" {
		t.Fatalf("defaults = %+v, want off + random port", cfg.Web)
	}
	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[web]\nserve = true\naddr = \"127.0.0.1:7777\"\n")
	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[web]\naddr = \"127.0.0.1:8888\"\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Web.Serve || cfg.Web.Addr != "127.0.0.1:8888" {
		t.Fatalf("repo over global = %+v", cfg.Web)
	}
}
```

```go
// internal/config/write_test.go
func TestSetWebServeAndAddr(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := SetWebServe(p, true); err != nil {
		t.Fatal(err)
	}
	if err := SetWebAddr(p, "127.0.0.1:7777"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p, filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Web.Serve || cfg.Web.Addr != "127.0.0.1:7777" {
		t.Fatalf("written config = %+v", cfg.Web)
	}
	if err := SetWebAddr(p, ""); err != nil { // clearing = back to a random port
		t.Fatal(err)
	}
	cfg, _ = Load(p, filepath.Join(t.TempDir(), "none.toml"))
	if cfg.Web.Addr != "" {
		t.Fatalf("addr after clear = %q", cfg.Web.Addr)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/config/ -run 'TestWebLayers|TestSetWebServeAndAddr' 2>&1 | tail -5`
Expected: compile errors `cfg.Web undefined`, `undefined: SetWebServe`.

- [ ] **Step 3: Implement**

`config.go`: add to `Config` after `Tasks`: `Web WebConfig \`toml:"web"\``; the struct:

```go
// WebConfig governs the web page the TUI can serve from its own process
// (standalone `gg web` ignores it: its flags rule there).
type WebConfig struct {
	Serve bool   `toml:"serve"` // start serving when the TUI launches (browser not opened); false = on demand via the palette's Open in browser
	Addr  string `toml:"addr"`  // listen address, loopback only; "" = 127.0.0.1:0 (a random port each run)
}

func overlayWeb(dst *WebConfig, src WebConfig) {
	if src.Serve {
		dst.Serve = true
	}
	if src.Addr != "" {
		dst.Addr = src.Addr
	}
}
```

Call `overlayWeb(&cfg.Web, layer.Web)` next to `overlayTasks` in `Load`. `Defaults()` needs nothing (zero = off, random). `template.go` `settingDocs`:

```go
{"web", "serve", nil, "serve the gg web page from the TUI process at startup so the browser and the terminal share agent sessions (default: false — start it on demand with the palette's Open in browser)"},
{"web", "addr", nil, "listen address for the hosted web page, loopback only (default: 127.0.0.1:0 = a random port; set e.g. \"127.0.0.1:7777\" for a stable bookmark)"},
```

and `template_test.go` `check("web", reflect.TypeOf(WebConfig{}))`. `write.go`:

```go
// SetWebServe persists `[web] serve` (the Settings "Web page" toggle).
func SetWebServe(path string, on bool) error { return setScalarLine(path, "web", "serve", strconv.FormatBool(on)) }

// SetWebAddr persists `[web] addr`; "" removes the line (back to a random port).
func SetWebAddr(path, addr string) error {
	if addr == "" {
		return removeScalarLine(path, "web", "addr")
	}
	return setScalarLine(path, "web", "addr", strconv.Quote(addr))
}
```

If `write.go` has no `removeScalarLine`, read how `SetWorktreePostCreateHook` clears (line 91) and reuse that mechanism; otherwise write the line as `addr = ""` (the overlay treats "" as unset, which is the same outcome) and drop the remove.

- [ ] **Step 4: Run the config tests**

Run: `go test ./internal/config/ 2>&1 | tail -5`
Expected: PASS including `TestSettingDocsCoverAllFields` and the populate tests.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat(config): [web] serve and addr for the TUI-hosted web page"
```

---

### Task 5: Hosted server rules — `hosted` flag, opener, `adoptService`, 409, the page hides switching

**Files:**
- Modify: `internal/web/server.go:20-125,236-259`, `internal/web/reroot.go:59-194`, `internal/web/static/ops.js:740-745`, `internal/web/static/palette.js:64-70`, `internal/web/static/sidebar.js:730-740`, `internal/web/static/locks.js:75-83`, `internal/web/static/core.js:17`
- Test: `internal/web/reroot_test.go` (existing), new `internal/web/hostedjs_test.go`

**Interfaces:**
- Consumes: `New(svc)` (unchanged signature).
- Produces: `(*Server).hosted bool`, `(*Server).opener func(string) *domain.Service`, `func (s *Server) adoptService(ctx, svc *domain.Service)`, `/api/repo` `"hosted"`.

- [ ] **Step 1: Write the failing Go tests**

Append to `internal/web/reroot_test.go` (its helpers build a repo with a worktree and POST `/api/reroot`):

```go
func TestRepoInfoReportsHosted(t *testing.T) {
	t.Parallel()
	srv := New(domain.Open(newRepoDir(t, 1)))
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	var info struct{ Hosted bool `json:"hosted"` }
	getJSONInto(t, ts.URL+"/api/repo", &info) // the file's JSON getter
	if info.Hosted {
		t.Fatal("a plain server is not hosted")
	}
	srv.hosted = true
	getJSONInto(t, ts.URL+"/api/repo", &info)
	if !info.Hosted {
		t.Fatal("hosted must be reported")
	}
}

func TestRerootEndpointRefusedWhileHosted(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.hosted = true
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	resp := postJSONRaw(t, ts.URL+"/api/reroot", map[string]any{"path": dir}) // the file's raw poster
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if !strings.Contains(readBody(t, resp), "terminal owns") {
		t.Fatal("the 409 names the terminal as the owner")
	}
}

func TestHandleRerootUsesTheOpener(t *testing.T) {
	// A worktree exists; the opener records the path it is asked for.
	dir, wt := repoWithWorktree(t) // the file's helper (read its name)
	srv := New(domain.Open(dir))
	t.Cleanup(srv.Close)
	var opened []string
	srv.opener = func(p string) *domain.Service { opened = append(opened, p); return domain.Open(p) }
	ts := serve(t, srv)
	postJSONOK(t, ts.URL+"/api/reroot", map[string]any{"path": wt})
	if len(opened) != 1 || opened[0] != wt {
		t.Fatalf("opener calls = %v, want [%s]", opened, wt)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/web/ -run 'TestRepoInfoReportsHosted|TestRerootEndpointRefusedWhileHosted|TestHandleRerootUsesTheOpener' 2>&1 | tail -6`
Expected: compile errors `srv.hosted undefined`, `srv.opener undefined`.

- [ ] **Step 3: Implement the server side**

`server.go`: fields

```go
	// hosted marks a page served by a TUI from its own process: the terminal
	// owns the current repository, so the page's own re-root is refused and
	// the SPA hides its switch affordances (/api/repo reports it).
	hosted bool
	// opener builds a Service for a path this server opens itself
	// (handleReroot's target). A TUI host passes domain.OpenTUI so an ssh
	// prompt can never reach its raw-mode terminal; nil = domain.Open.
	opener func(string) *domain.Service
```

`func (s *Server) open(path string) *domain.Service { if s.opener != nil { return s.opener(path) }; return domain.Open(path) }`. `writeRepoInfo` gains `s` access: change its signature to a method `func (s *Server) writeRepoInfo(w, r, svc)` (two callers: `handleRepo` and `handleReroot`) and add `"hosted": s.hosted`.

`reroot.go` `handleReroot`: first lines

```go
	if s.hosted {
		writeErr(w, http.StatusConflict, errors.New("the terminal owns the current repository — switch there"))
		return
	}
```

`cand := domain.Open(target)` → `cand := s.open(target)`. Lift the tail (from the `s.opMu.Lock()` before `s.svc.Store(cand)` through `s.rehomeSteerPresence`) into:

```go
// adoptService makes svc the served repository: the swap under opMu, the
// dropped op record and feed, a fresh live hub (tabs reconnect), the MRU
// touch and the presence re-home. handleReroot and a TUI host's Reroot
// both end here. It reports errOpBusy when an operation is live.
func (s *Server) adoptService(ctx context.Context, svc *domain.Service) error {
	s.opMu.Lock()
	if s.cur != nil {
		s.cur.mu.Lock()
		live := !s.cur.done
		s.cur.mu.Unlock()
		if live {
			s.opMu.Unlock()
			return errOpBusy
		}
	}
	s.svc.Store(svc)
	s.cur = nil
	s.opMu.Unlock()
	s.mu.Lock()
	s.feed = nil
	s.mu.Unlock()
	s.restartLive(ctx)
	touchMRU(ctx, svc, s.reposStatePath())
	s.rehomeSteerPresence(ctx, svc)
	return nil
}
```

`handleReroot` keeps `applyUIPolicies(r.Context(), cand, …)` before calling `adoptService` (standalone re-applies; a host does not — the TUI already did) and ends with `s.writeRepoInfo(w, r, cand)`.

- [ ] **Step 4: Run the Go tests**

Run: `go test ./internal/web/ -run 'Reroot|RepoInfo' 2>&1 | tail -5`
Expected: PASS (the existing re-root tests still pass through `adoptService`).

- [ ] **Step 5: Write the failing JS wiring test**

```go
// internal/web/hostedjs_test.go
package web

import (
	"strings"
	"testing"
)

// The hosted page hides its four repo-switch affordances; the strings below
// are the wiring pins (the pure logic is one `state.hosted` gate each).
func TestHostedPageHidesRepoSwitching(t *testing.T) {
	t.Parallel()
	cases := []struct{ file, want string }{
		{"ops.js", "state.hosted = !!repo.hosted"},
		{"palette.js", `if (!state.hosted) cmds.push({ label: "switch repo…"`},
		{"palette.js", `if (!state.hosted) cmds.push({ label: "open repo (path)…"`},
		{"sidebar.js", "if (!state.hosted && !(state.worktree && w.path === state.worktree))"},
		{"locks.js", "if (served && !state.hosted) doReroot(to);"},
		{"core.js", "hosted: false,"},
	}
	for _, c := range cases {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s: missing %q", c.file, c.want)
		}
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/web/ -run TestHostedPageHidesRepoSwitching 2>&1 | tail -8`
Expected: FAIL on all six pins.

- [ ] **Step 7: Implement the page side**

`core.js` `state`: add `hosted: false, // /api/repo: a TUI serves this page and owns the repo (switching is hidden)`. `ops.js` `loadRepo`: after `state.repo = repo;` add `state.hosted = !!repo.hosted;`. `palette.js`: the command list is a literal array; turn the two rows into conditional pushes exactly as the pins say (build `const cmds = [...]` then `if (!state.hosted) cmds.push({ label: "switch repo…", detail: "", run: null });` and the `open repo (path)…` row; keep the alphabetical/positional order the palette expects — read `runPaletteRow` for how `run: null` is matched, it is by label, so position is free). `sidebar.js` `showWorktreeMenu`: gate `switch here` with `!state.hosted &&`. `locks.js`: `if (served && !state.hosted) doReroot(to); else refreshAfterOp();`.

- [ ] **Step 8: Run the web tests**

Run: `go test ./internal/web/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/web
git commit -m "feat(web): hosted servers — /api/repo hosted flag, opener seam, adoptService, 409 re-root, hidden switching on the page"
```

---

### Task 6: `Host` — start, re-root, close; `Serve` runs on it

**Files:**
- Create: `internal/web/host.go`, `internal/web/host_test.go`
- Modify: `internal/web/serve.go:26-100`, `internal/web/steer.go:401-447`
- Test: `internal/web/serve_test.go` (existing tests keep passing)

**Interfaces:**
- Consumes: `New`, `listen`, `startLive`, `startOpenFilesWatch`, `initSteerPresence`, `announceShutdown`, `Close`, `removeSteerPresence`, `adoptService` (task 5), `openBrowser`.
- Produces:

```go
type Host struct { ... }
func NewHost(svc *domain.Service, opener func(string) *domain.Service, hosted bool) *Host
func (h *Host) Start(ctx context.Context, addr string) (string, error)
func (h *Host) Reroot(ctx context.Context, svc *domain.Service) error
func (h *Host) URL() string
func (h *Host) OpenBrowser()
func (h *Host) Close()
var ErrPageLive = errors.New("another gg web page is already serving this worktree") // wrapped with the URL
```

- [ ] **Step 1: Write the failing host tests**

```go
// internal/web/host_test.go
package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func repoInfo(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url + "/api/repo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func webPresence(t *testing.T, svc *domain.Service) (steer.Presence, bool) {
	t.Helper()
	dir, _ := resolveSteerDir(context.Background(), svc)
	if dir == "" {
		t.Fatal("steering resolved off")
	}
	return steer.Live(dir, steer.WebPresence)
}

func TestHostStartServesHosted(t *testing.T) {
	isolateState(t) // XDG_STATE_HOME to a temp dir so web.json lands in the test's own inbox (write this helper next to isolateGlobal if absent)
	svc := domain.Open(newRepoDir(t, 1))
	h := NewHost(svc, domain.Open, true)
	url, err := h.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	if !strings.HasPrefix(url, "http://127.0.0.1:") || h.URL() != url {
		t.Fatalf("url = %q", url)
	}
	if info := repoInfo(t, url); info["hosted"] != true {
		t.Fatalf("hosted = %v", info["hosted"])
	}
	if p, live := webPresence(t, svc); !live || p.URL != url {
		t.Fatalf("web.json = %+v live=%v, want this URL", p, live)
	}
}

func TestHostRerootFollows(t *testing.T) {
	isolateState(t)
	dir, wt := repoWithWorktree(t)
	svc := domain.Open(dir)
	h := NewHost(svc, domain.Open, true)
	url, err := h.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	next := domain.Open(wt)
	if err := h.Reroot(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if got := repoInfo(t, url)["worktree"]; got != wt {
		t.Fatalf("worktree after reroot = %v, want %s", got, wt)
	}
	if _, live := webPresence(t, svc); live {
		t.Fatal("the old inbox must lose web.json")
	}
	if p, live := webPresence(t, next); !live || p.URL != url {
		t.Fatalf("new inbox web.json = %+v live=%v", p, live)
	}
	// Twice in a row lands on the last one.
	if err := h.Reroot(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if got := repoInfo(t, url)["worktree"]; got != svc.Root() {
		t.Fatalf("worktree after second reroot = %v", got)
	}
}

func TestHostRefusesForeignPage(t *testing.T) {
	isolateState(t)
	svc := domain.Open(newRepoDir(t, 1))
	dir, _ := resolveSteerDir(context.Background(), svc)
	if err := steer.Touch(dir, steer.WebPresence, steer.Presence{PID: 99999, URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	h := NewHost(svc, domain.Open, true)
	_, err := h.Start(context.Background(), "127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "http://127.0.0.1:1") {
		t.Fatalf("err = %v, want a refusal naming the other page", err)
	}
	if p, live := steer.Live(dir, steer.WebPresence); !live || p.PID != 99999 {
		t.Fatal("the foreign presence must be left alone")
	}
}

func TestHostCloseAnnouncesShutdown(t *testing.T) {
	isolateState(t)
	svc := domain.Open(newRepoDir(t, 1))
	h := NewHost(svc, domain.Open, true)
	url, err := h.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	events := openEventsStream(t, url) // reuse shutdown_test.go's reader; read its helper name first
	start := time.Now()
	h.Close()
	waitForEvent(t, events, "shutdown", shutdownGrace+time.Second)
	if time.Since(start) > shutdownGrace+time.Second {
		t.Fatal("Close must return within the grace")
	}
	if _, live := webPresence(t, svc); live {
		t.Fatal("Close removes web.json")
	}
	if _, err := http.Get(url + "/api/repo"); err == nil {
		t.Fatal("the port must be closed")
	}
}

func TestServeStandaloneIsNotHosted(t *testing.T) {
	// Serve over a temp repo on a random port, read /api/repo, cancel.
	isolateState(t)
	dir := newRepoDir(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	urlCh := make(chan string, 1)
	serveURLHook = func(u string) { urlCh <- u } // a package var Serve calls after listen (test seam; nil in production)
	t.Cleanup(func() { serveURLHook = nil })
	go func() { _ = Serve(ctx, dir, "127.0.0.1:0", false, nil) }()
	select {
	case u := <-urlCh:
		if repoInfo(t, u)["hosted"] != false {
			t.Fatal("standalone is not hosted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve never bound")
	}
}
```

`isolateState`: `t.Setenv("XDG_STATE_HOME", t.TempDir())` (check `config.SessionSteerDir` reads it — it does through `repos.DefaultStatePath`'s sibling; verify with `grep -n XDG_STATE_HOME internal/config/*.go`).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/web/ -run 'TestHost|TestServeStandalone' 2>&1 | tail -6`
Expected: compile errors `undefined: NewHost`, `undefined: serveURLHook`.

- [ ] **Step 3: Implement the host**

```go
// internal/web/host.go
package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// ErrPageLive: another gg web page already serves this worktree (its URL is
// in the wrapped error). A host never overwrites a live presence.
var ErrPageLive = errors.New("another gg web page is already serving this worktree")

// Host is one served page over a caller-owned Service: the reusable core of
// gg web. Serve wraps it for the standalone command; the TUI hosts it
// in-process (cmd/gg wires tui.NewWebHost to NewHost) so both frontends
// read one session manager.
type Host struct {
	srv     *Server
	httpSrv *http.Server
	url     string
	mu      sync.Mutex
	done    chan struct{}
	once    sync.Once
}

// NewHost builds the server over svc. opener opens a Service for a path
// the server chooses itself (nil = domain.Open); hosted marks the page as
// terminal-owned.
func NewHost(svc *domain.Service, opener func(string) *domain.Service, hosted bool) *Host {
	s := New(svc)
	s.opener = opener
	s.hosted = hosted
	return &Host{srv: s, done: make(chan struct{})}
}

// Start binds addr ("" = 127.0.0.1:0), starts the live hub, the open-files
// watch and the steering presence, and serves on a goroutine. It refuses
// to start when a foreign web.json is live for this worktree.
func (h *Host) Start(ctx context.Context, addr string) (string, error) {
	if dir, _ := resolveSteerDir(ctx, h.srv.service()); dir != "" {
		if p, live := steer.Live(dir, steer.WebPresence); live && p.PID != os.Getpid() {
			return "", fmt.Errorf("%w at %s", ErrPageLive, p.URL)
		}
	}
	ln, url, err := listen(addr)
	if err != nil {
		return "", err
	}
	h.mu.Lock()
	h.url = url
	h.mu.Unlock()
	h.srv.startLive(ctx)
	h.srv.startOpenFilesWatch()
	h.srv.initSteerPresence(ctx, url)
	h.httpSrv = &http.Server{Handler: h.srv.Handler()}
	go func() {
		defer close(h.done)
		_ = h.httpSrv.Serve(ln) // ErrServerClosed on Close
	}()
	return url, nil
}

// Reroot points the page at svc (the TUI's new repository).
func (h *Host) Reroot(ctx context.Context, svc *domain.Service) error {
	return h.srv.adoptService(ctx, svc)
}

// URL is the served address ("" before Start).
func (h *Host) URL() string { h.mu.Lock(); defer h.mu.Unlock(); return h.url }

// OpenBrowser opens the system browser at the URL (best-effort).
func (h *Host) OpenBrowser() {
	if u := h.URL(); u != "" {
		openBrowser(u)
	}
}

// Close tells every tab the server is going, shuts the listener down within
// shutdownGrace, releases the hub and removes the presence. Idempotent.
func (h *Host) Close() {
	h.once.Do(func() {
		if h.httpSrv == nil {
			h.srv.Close()
			return
		}
		h.srv.announceShutdown()
		sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = h.httpSrv.Shutdown(sctx)
		<-h.done
		h.srv.Close()
		h.srv.removeSteerPresence()
	})
}
```

`serve.go` `Serve`: keep everything through `srv := New(svc)` + `setStartAt`, then replace the listen/startLive/…/httpSrv block with a host built over the already-configured server. To avoid constructing twice, add an unexported `newHostOver(s *Server) *Host` that `NewHost` also uses; `Serve` does:

```go
	h := newHostOver(srv)
	url, err := h.Start(ctx, addr)
	if err != nil {
		return err
	}
	if serveURLHook != nil {
		serveURLHook(url)
	}
	fmt.Fprintln(os.Stderr, "gg web: serving", url)
	if launch {
		openBrowser(url)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	<-ctx.Done()
	h.Close()
	return nil
```

`var serveURLHook func(string)` with a comment (test seam). Drop the now-unused `defer srv.Close()`/`defer srv.removeSteerPresence()` (Close does both). Note `Host.Start` refuses a foreign presence: standalone `gg web` today overwrites it; keep that behaviour for standalone by having `newHostOver` set `h.refuseForeign = hosted` and check the flag in `Start` (a second `gg web` in one worktree was allowed before and stays allowed).

- [ ] **Step 4: Run the web tests**

Run: `go test ./internal/web/ 2>&1 | tail -5`
Expected: PASS, including `TestServePreflight*`, `TestEventsAnnounceShutdown`, the new host tests.

- [ ] **Step 5: Commit**

```bash
git add internal/web
git commit -m "feat(web): Host — the reusable core of gg web (start, re-root, close); Serve runs on it"
```

---

### Task 7: The TUI seam — palette entry, status, startup, re-root, quit

**Files:**
- Create: `internal/tui/webhost.go`, `internal/tui/webhost_test.go`
- Modify: `internal/tui/model.go` (Model fields near 128, `New` at 437, `Init` at 477, `Update` message arms near 570, `reRoot` at 4456 after `m.svc = domain.OpenTUI(path)`), `internal/tui/run.go:25-110`, `internal/tui/command_palette.go:53-85`, `internal/tui/help.go:38`, `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/webhost_test.go`

**Interfaces:**
- Consumes: `config.WebConfig` (task 4). Nothing from `internal/web`.
- Produces:

```go
type WebHost interface {
	Start(ctx context.Context, addr string) (string, error)
	Reroot(ctx context.Context, svc *domain.Service) error
	URL() string
	OpenBrowser()
	Close()
}
var NewWebHost func(svc *domain.Service) WebHost
type RunOptions struct { RecordPath string; At model.Link; Web bool; WebAddr string }
func Run(svc *domain.Service, opts RunOptions) (string, error)
func (m Model) webAddr() string          // flag > [web] addr > ""
func (m Model) openInBrowser() (Model, tea.Cmd)
func (m Model) webStatusText() string    // "not running" | "starting…" | the URL
```

- [ ] **Step 1: Write the failing tests with a recording fake**

```go
// internal/tui/webhost_test.go
package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

type fakeWebHost struct {
	starts, opens, closes int
	addr                  string
	reroots               []*domain.Service
	url                   string
	startErr              error
}

func (f *fakeWebHost) Start(_ context.Context, addr string) (string, error) {
	f.starts++
	f.addr = addr
	if f.startErr != nil {
		return "", f.startErr
	}
	f.url = "http://127.0.0.1:4242"
	return f.url, nil
}
func (f *fakeWebHost) Reroot(_ context.Context, svc *domain.Service) error { f.reroots = append(f.reroots, svc); return nil }
func (f *fakeWebHost) URL() string                                          { return f.url }
func (f *fakeWebHost) OpenBrowser()                                         { f.opens++ }
func (f *fakeWebHost) Close()                                               { f.closes++ }

func installFakeHost(t *testing.T) *fakeWebHost {
	t.Helper()
	f := &fakeWebHost{}
	NewWebHost = func(*domain.Service) WebHost { return f }
	t.Cleanup(func() { NewWebHost = nil })
	return f
}

func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg := cmd()
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func TestOpenInBrowserStartsOnceThenReopens(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	m = runCmd(t, m, cmd) // webStartedMsg
	if f.starts != 1 || f.opens != 1 || m.statusMsg != "web page: serving http://127.0.0.1:4242" {
		t.Fatalf("starts=%d opens=%d status=%q", f.starts, f.opens, m.statusMsg)
	}
	m, cmd = m.openInBrowser()
	if cmd != nil {
		t.Fatal("a second use only opens the browser")
	}
	if f.starts != 1 || f.opens != 2 {
		t.Fatalf("starts=%d opens=%d", f.starts, f.opens)
	}
}

func TestOpenInBrowserBindFailureIsReportedAndRetriable(t *testing.T) {
	f := installFakeHost(t)
	f.startErr = errors.New("listen tcp 127.0.0.1:7777: bind: address already in use")
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	m = runCmd(t, m, cmd)
	if m.web.host != nil || !strings.Contains(m.statusMsg, "address already in use") {
		t.Fatalf("host kept=%v status=%q", m.web.host != nil, m.statusMsg)
	}
	f.startErr = nil
	m, cmd = m.openInBrowser()
	m = runCmd(t, m, cmd)
	if f.starts != 2 || m.web.host == nil {
		t.Fatal("the palette entry retries")
	}
}

func TestWebAddrFlagBeatsConfig(t *testing.T) {
	m := loadedModel(t)
	m.cfg.Web = config.WebConfig{Addr: "127.0.0.1:1111"}
	if got := m.webAddr(); got != "127.0.0.1:1111" {
		t.Fatalf("config addr = %q", got)
	}
	m.webOpts.WebAddr = "127.0.0.1:2222"
	if got := m.webAddr(); got != "127.0.0.1:2222" {
		t.Fatalf("flag addr = %q", got)
	}
}

func TestServeAtStartup(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m.cfg.Web.Serve = true
	cmd := m.startupWebCmd()
	if cmd == nil {
		t.Fatal("[web] serve starts the host at launch")
	}
	m = runCmd(t, m, cmd)
	if f.starts != 1 || f.opens != 0 {
		t.Fatalf("starts=%d opens=%d — startup serving never opens the browser", f.starts, f.opens)
	}
	m2 := loadedModel(t)
	m2.webOpts.Web = true
	if m2.startupWebCmd() == nil {
		t.Fatal("--web starts the host at launch")
	}
	m3 := loadedModel(t)
	if m3.startupWebCmd() != nil {
		t.Fatal("neither set: nothing starts")
	}
}

func TestReRootHandsTheNewServiceToTheHost(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m, cmd := m.openInBrowser()
	m = runCmd(t, m, cmd)
	other := newRepoDir(t) // the tui test helper for a second repo
	nm, cmd := m.reRoot(other)
	m = nm.(Model)
	// reRoot batches many cmds; run them until the reroot result lands.
	drainCmds(t, m, cmd) // helper: executes a tea.Batch tree, feeding msgs back (write it if the file has none)
	if len(f.reroots) != 1 || f.reroots[0] != m.svc {
		t.Fatalf("reroots = %d, want the new service", len(f.reroots))
	}
}

func TestPaletteHasOpenInBrowserOnlyWithASeam(t *testing.T) {
	m := loadedModel(t)
	if paletteHas(m, "Open in browser") {
		t.Fatal("no seam: no entry")
	}
	installFakeHost(t)
	if !paletteHas(m, "Open in browser") {
		t.Fatal("seam set: entry present")
	}
}
```

`paletteHas` iterates `m.availablePaletteCommands()` labels. `drainCmds`: look for an existing helper (`runBatch`, `collectMsgs`) in the tui tests before writing one.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'OpenInBrowser|WebAddr|ServeAtStartup|ReRootHandsTheNewService|PaletteHasOpenInBrowser' 2>&1 | tail -6`
Expected: compile errors `undefined: WebHost`, `m.openInBrowser undefined`.

- [ ] **Step 3: Implement**

```go
// internal/tui/webhost.go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// WebHost is the gg web page served from THIS process. cmd/gg sets
// NewWebHost (internal/web's Host); nil = unavailable, and the palette
// entry is absent. Sharing the process is the point: the page and the
// terminal read one agent-session manager.
type WebHost interface {
	Start(ctx context.Context, addr string) (string, error)
	Reroot(ctx context.Context, svc *domain.Service) error
	URL() string
	OpenBrowser()
	Close()
}

// NewWebHost builds a host over the TUI's Service. Set by cmd/gg.
var NewWebHost func(svc *domain.Service) WebHost

// webHostState lives on a pointer field: a start in flight and a running
// host must survive the value-receiver copy.
type webHostState struct {
	host     WebHost
	url      string
	starting bool
}

// webLaunchOptions carries `gg --web` / `--web-addr` (RunOptions).
type webLaunchOptions struct {
	Web     bool
	WebAddr string
}

type webStartedMsg struct {
	host WebHost
	url  string
	err  error
	open bool // open the browser once serving (the palette path; startup does not)
}

func (m Model) ensureWeb() Model {
	if m.web == nil {
		m.web = &webHostState{}
	}
	return m
}

// webAddr is the address to bind: the launch flag, else [web] addr, else
// "" (the host's random loopback port).
func (m Model) webAddr() string {
	if m.webOpts.WebAddr != "" {
		return m.webOpts.WebAddr
	}
	return m.cfg.Web.Addr
}

func (m Model) webServing() bool { return m.web != nil && m.web.host != nil }

func startWebCmd(svc *domain.Service, addr string, open bool) tea.Cmd {
	return func() tea.Msg {
		h := NewWebHost(svc)
		url, err := h.Start(context.Background(), addr)
		if err != nil {
			return webStartedMsg{err: err, open: open}
		}
		return webStartedMsg{host: h, url: url, open: open}
	}
}

// openInBrowser is the palette's "Open in browser": start serving on first
// use, reopen the browser afterwards.
func (m Model) openInBrowser() (Model, tea.Cmd) {
	m = m.ensureWeb()
	switch {
	case NewWebHost == nil:
		m.statusMsg = i18n.T("this gg cannot serve a web page")
		return m, nil
	case m.web.host != nil:
		m.web.host.OpenBrowser()
		m.statusMsg = i18n.T("web page: %s", m.web.url)
		return m, nil
	case m.web.starting:
		m.statusMsg = i18n.T("web page: starting…")
		return m, nil
	}
	m.web.starting = true
	m.statusMsg = i18n.T("web page: starting…")
	return m, startWebCmd(m.svc, m.webAddr(), true)
}

// startupWebCmd serves at launch when [web] serve or --web asks for it.
func (m Model) startupWebCmd() tea.Cmd {
	if NewWebHost == nil || !(m.cfg.Web.Serve || m.webOpts.Web) {
		return nil
	}
	m.web.starting = true // m.web is a pointer: the flag survives
	return startWebCmd(m.svc, m.webAddr(), false)
}

func (m Model) onWebStarted(msg webStartedMsg) (Model, tea.Cmd) {
	m = m.ensureWeb()
	m.web.starting = false
	if msg.err != nil {
		m.statusMsg = i18n.T("web page: %s", msg.err.Error())
		return m, nil
	}
	m.web.host, m.web.url = msg.host, msg.url
	m.statusMsg = i18n.T("web page: serving %s", msg.url)
	if msg.open {
		msg.host.OpenBrowser()
	}
	return m, nil
}

// webStatusText is the Settings row value.
func (m Model) webStatusText() string {
	switch {
	case m.webServing():
		return m.web.url
	case m.web != nil && m.web.starting:
		return i18n.T("starting…")
	}
	return i18n.T("not running")
}

type webRerootMsg struct{ err error }

func rerootWebCmd(h WebHost, svc *domain.Service) tea.Cmd {
	return func() tea.Msg { return webRerootMsg{err: h.Reroot(context.Background(), svc)} }
}

// closeWeb ends the hosted page (Run's exit path).
func (m Model) closeWeb() {
	if m.webServing() {
		m.web.host.Close()
	}
}
```

Model: fields `web *webHostState` (initialised in `New` to `&webHostState{}`) and `webOpts webLaunchOptions`. `Update`: arms `case webStartedMsg: return m.onWebStarted(msg)` and `case webRerootMsg: if msg.err != nil { m.statusMsg = i18n.T("web page: %s", msg.err.Error()) }; return m, nil`. `Init`: append `m.startupWebCmd()` to the batch (nil cmds are fine in `tea.Batch`). `reRoot`: after `m.svc = domain.OpenTUI(path)`, collect `rerootWebCmd(m.web.host, m.svc)` when `m.webServing()` into the cmd it returns (read how reRoot batches its returned cmd — it ends with a `tea.Batch`; add the cmd there). Palette entry in `paletteCommands()` between "Open gg:// link…" and "Open repo":

```go
		{label: i18n.T("Open in browser"), web: true, run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openInBrowser() }},
```

with a `web bool` field on `paletteCommand` and, in `availablePaletteCommands`, `if c.web && NewWebHost == nil { continue }`. `help.go:38`: add "Open in browser" to the palette row's list (between "Open gg:// link" and "Open repo") — the whole key string changes, so update the four bundles' translations of that row (translate the new item as the bundles translate "Open in browser": ja "ブラウザで開く", ko "브라우저에서 열기", zh "在浏览器中打开", ru "Открыть в браузере"). New keys for all four bundles: "Open in browser", "this gg cannot serve a web page", "web page: %s", "web page: starting…", "web page: serving %s", "starting…", "not running".

`run.go`: `Run(svc *domain.Service, opts RunOptions) (string, error)` with `type RunOptions struct { RecordPath string; At model.Link; Web bool; WebAddr string }`; `m.webOpts = webLaunchOptions{Web: opts.Web, WebAddr: opts.WebAddr}` set before `p := tea.NewProgram`; after the KillAll block: `fm.closeWeb()` inside the `if fm, ok := final.(Model)` branch. Update the one caller in `cmd/gg/main.go` (`tui.Run(svc, tui.RunOptions{RecordPath: recordPath, At: at})` for now; task 9 adds the flags).

- [ ] **Step 4: Run the TUI tests and the i18n gates**

Run: `go build ./... && go test ./internal/tui/ 2>&1 | tail -8`
Expected: PASS including `TestHelpFooterCoverage`, the i18n scan tests, the palette tests and the new file.

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/i18n cmd/gg/main.go
git commit -m "feat(tui): serve the web page from the TUI process — Open in browser, [web] serve/--web startup, re-root follows, quit closes"
```

---

### Task 8: Settings popup — the "Web page" row and its editor

**Files:**
- Create: `internal/tui/web_settings_popup.go`, `internal/tui/web_settings_popup_test.go`
- Modify: `internal/tui/settings_popup.go:51-72,93-121,179-200,538-565`, `internal/i18n/lang/{ja,ko,zh,ru}.toml`

**Interfaces:**
- Consumes: `m.webStatusText()`, `m.openInBrowser()` (task 7), `config.SetWebServe`, `config.SetWebAddr` (task 4), `newTextField` (hook_editor.go), `m.repoConfigPath`, `config.DefaultGlobalPath()`.
- Produces: `settingsMenuWeb = "Web page"`, `webSettingsPopup` layer.

- [ ] **Step 1: Write the failing tests**

```go
// internal/tui/web_settings_popup_test.go
package tui

func TestSettingsWebRowShowsState(t *testing.T) {
	m := loadedModel(t)
	if got := m.settingsRowText(settingsMenuWeb); !strings.Contains(got, "not running") {
		t.Fatalf("row = %q", got)
	}
	installFakeHost(t)
	m, cmd := m.openInBrowser()
	m = runCmd(t, m, cmd)
	if got := m.settingsRowText(settingsMenuWeb); !strings.Contains(got, "http://127.0.0.1:4242") {
		t.Fatalf("row = %q", got)
	}
}

func TestWebSettingsPopupTogglesServeAndEditsAddr(t *testing.T) {
	global := isolateGlobalConfig(t) // the tui tests' XDG_CONFIG_HOME helper (grep for one; write it if absent)
	m := loadedModel(t)
	m = m.openWebSettings()
	p := layerOf[*webSettingsPopup](m)
	if p == nil {
		t.Fatal("popup pushed")
	}
	// Row 0 = Serve at startup: enter toggles and writes the global config.
	m, _ = p.update(m, keyMsg("enter"))
	if !m.cfg.Web.Serve {
		t.Fatal("serve toggled on")
	}
	if b, _ := os.ReadFile(global); !strings.Contains(string(b), "serve = true") {
		t.Fatalf("global config = %q", b)
	}
	// Row 1 = Address: enter opens the field, typing + enter saves.
	m, _ = p.update(m, keyMsg("down"))
	m, _ = p.update(m, keyMsg("enter"))
	for _, r := range "127.0.0.1:7777" {
		m, _ = p.update(m, keyMsg(string(r)))
	}
	m, _ = p.update(m, keyMsg("enter"))
	if m.cfg.Web.Addr != "127.0.0.1:7777" {
		t.Fatalf("addr = %q", m.cfg.Web.Addr)
	}
	if b, _ := os.ReadFile(global); !strings.Contains(string(b), `addr = "127.0.0.1:7777"`) {
		t.Fatalf("global config = %q", b)
	}
	// esc closes back to Settings.
	m, _ = p.update(m, keyMsg("esc"))
	if layerOf[*webSettingsPopup](m) != nil {
		t.Fatal("esc pops the editor")
	}
}
```

`settingsRowText` is whatever `settings_popup.go` names the function at line 179 that renders `title + ": " + value` (read it; call the real name).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'SettingsWebRow|WebSettingsPopup' 2>&1 | tail -5`
Expected: compile errors `undefined: settingsMenuWeb`, `m.openWebSettings undefined`.

- [ ] **Step 3: Implement**

`settings_popup.go`: `settingsMenuWeb = "Web page"` appended to the consts and to `settingsMenu` after `settingsMenuOpsHist`; title case → `i18n.T("Web page")`; value case → `title + ": " + m.webStatusText()`; enter case → `return m.openWebSettings(), nil`.

```go
// internal/tui/web_settings_popup.go
package tui

// webSettingsPopup is the Settings "Web page" editor: the URL line, the
// [web] serve toggle, the [web] addr field and an Open in browser action.
// Both settings are GLOBAL (the page is per human, like the theme).
type webSettingsPopup struct {
	sel     int
	editing bool
	buf     *textField
}

const (
	webRowServe = iota
	webRowAddr
	webRowOpen
	webRowCount
)

func (m Model) openWebSettings() Model {
	return m.pushLayer(&webSettingsPopup{})
}

func (p *webSettingsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if p.editing {
		switch msg.String() {
		case "esc":
			p.editing, p.buf = false, nil
			return m, nil
		case "enter":
			addr := strings.TrimSpace(p.buf.value())
			p.editing, p.buf = false, nil
			return m.setWebAddr(addr), nil
		case "ctrl+c":
			return m, tea.Quit
		}
		p.buf.update(msg)
		return m, nil
	}
	switch msg.String() {
	case "esc":
		return m.popLayer(), nil
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < webRowCount-1 {
			p.sel++
		}
	case "enter", " ":
		switch p.sel {
		case webRowServe:
			return m.toggleWebServe(), nil
		case webRowAddr:
			p.editing, p.buf = true, newTextField(m.cfg.Web.Addr)
		case webRowOpen:
			return m.openInBrowser()
		}
	}
	return m, nil
}

func (m Model) toggleWebServe() Model {
	next := !m.cfg.Web.Serve
	m.cfg.Web.Serve = next
	if err := config.SetWebServe(config.DefaultGlobalPath(), next); err != nil {
		m.statusMsg = i18n.T("web page: serve at startup → %s (not saved: %s)", onOff(next), err.Error())
	} else {
		m.statusMsg = i18n.T("web page: serve at startup %s", onOff(next))
	}
	return m
}

func (m Model) setWebAddr(addr string) Model {
	m.cfg.Web.Addr = addr
	if err := config.SetWebAddr(config.DefaultGlobalPath(), addr); err != nil {
		m.statusMsg = i18n.T("web page: address not saved: %s", err.Error())
		return m
	}
	if addr == "" {
		m.statusMsg = i18n.T("web page: address → a random port (next start)")
	} else {
		m.statusMsg = i18n.T("web page: address → %s (next start)", addr)
	}
	return m
}

func (p *webSettingsPopup) render(m Model, below string) string {
	w, h := m.width, m.height
	rows := []string{
		i18n.T("URL: %s", m.webStatusText()),
		"",
	}
	mark := func(i int, text string) string {
		if p.sel == i {
			return selectedRow.Render("> " + text)
		}
		return "  " + text
	}
	rows = append(rows, mark(webRowServe, i18n.T("Serve at startup: %s", onOff(m.cfg.Web.Serve))))
	addr := m.cfg.Web.Addr
	if addr == "" {
		addr = i18n.T("random port")
	}
	if p.editing {
		rows = append(rows, "> "+i18n.T("Address: ")+p.buf.view())
	} else {
		rows = append(rows, mark(webRowAddr, i18n.T("Address: %s", addr)))
	}
	rows = append(rows, mark(webRowOpen, i18n.T("Open in browser")), "", i18n.T("[enter] change  [esc] back"))
	box := modalStyle.Width(popupInnerWidth(w)).Render(titleStyle.Render(i18n.T("Web page")) + "\n" + strings.Join(rows, "\n"))
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
```

Match `textField`'s real method names (`value`, `view`, `update` — read `hook_editor.go` and the field's file) and the style names (`selectedRow`, `modalStyle`, `titleStyle`) used by neighbouring popups. All new strings into the four bundles.

- [ ] **Step 4: Run the TUI tests**

Run: `go test ./internal/tui/ 2>&1 | tail -6`
Expected: PASS including `settings_popup_test.go` and the i18n gates.

- [ ] **Step 5: Commit**

```bash
git add internal/tui internal/i18n
git commit -m "feat(tui): Settings → Web page: URL, serve at startup, address, Open in browser"
```

---

### Task 9: `serve` inbox command, `gg open --web` with a live TUI, `gg --web`/`--web-addr`

**Files:**
- Modify: `internal/steer/steer.go:81` (comment), `internal/tui/steer.go:361-412`, `internal/cli/open.go:134-185`, `cmd/gg/main.go:40-52,150-160,236-262`
- Create: `cmd/gg/webflags_test.go`
- Test: `internal/tui/steer_test.go`, `internal/cli/open_web_test.go`

**Interfaces:**
- Consumes: `m.openInBrowser`-style start (`startWebCmd`, `m.webServing()`, task 7); `steer.Post/AwaitReply`; `tui.RunOptions`, `tui.NewWebHost`, `web.NewHost` (task 6).
- Produces: `steer.Command{Cmd: "serve"}` → `Reply{OK, Detail: url}`; `cli.askTUIToServe(dir string, stderr io.Writer) (url string, ok bool)`; `extractWebFlags(args) (web bool, addr string, rest []string)`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/steer_test.go`:

```go
func TestSteerServeStartsTheHostAndRepliesWithTheURL(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m = m.withTestInbox(t) // the file's helper that gives the model a temp steerDir (read its name)
	nm, cmd := m.applySteer(steer.Command{ID: "c-s", Cmd: "serve", Wait: true})
	nm = runCmd(t, nm, cmd) // webStartedMsg → answers the command
	rep := readReply(t, nm.steerDir, "c-s") // the file's reply reader
	if !rep.OK || rep.Detail != "http://127.0.0.1:4242" || f.starts != 1 {
		t.Fatalf("reply = %+v starts=%d", rep, f.starts)
	}
	// Already serving: answers at once, no second start.
	nm, cmd = nm.applySteer(steer.Command{ID: "c-s2", Cmd: "serve", Wait: true})
	if cmd != nil {
		nm = runCmd(t, nm, cmd)
	}
	if rep := readReply(t, nm.steerDir, "c-s2"); !rep.OK || rep.Detail != "http://127.0.0.1:4242" || f.starts != 1 {
		t.Fatalf("reply = %+v starts=%d", rep, f.starts)
	}
}

func TestSteerServeWithoutASeamFails(t *testing.T) {
	m := loadedModel(t).withTestInbox(t)
	nm, cmd := m.applySteer(steer.Command{ID: "c-n", Cmd: "serve", Wait: true})
	if cmd != nil {
		runCmd(t, nm, cmd)
	}
	if rep := readReply(t, nm.steerDir, "c-n"); rep.OK || !strings.Contains(rep.Error, "cannot serve") {
		t.Fatalf("reply = %+v", rep)
	}
}
```

Append to `internal/cli/open_web_test.go`, replacing `TestOpenWebStartsAServerBesideALiveTUI` (its premise is gone):

```go
// A TUI live and no page: --web asks the TUI to serve, then posts the link
// to the page the reply names. No second server is started.
func TestOpenWebAsksALiveTUIToServe(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	// A stand-in TUI: answer the serve command by writing web.json + the reply.
	go answerServe(t, inbox, srv.URL) // helper: poll steer.Drain(inbox) for cmd "serve", then liveWebPresence + steer.PostReply{OK, Detail: url}
	calls := 0
	LaunchWeb = func(string, steer.Command) int { calls++; return 0 }
	t.Cleanup(func() { LaunchWeb = nil })
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + ":1"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if got := fake.commands(); len(got) != 1 {
		t.Fatalf("posted to the page = %+v, want one navigate", got)
	}
	if calls != 0 {
		t.Fatal("a live TUI serves; no standalone server is started")
	}
}

func TestOpenWebServeTimeoutExitsOne(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox) // nobody answers
	t.Cleanup(func() { steer.Discard(inbox) })
	old := steerReplyWaitForTest
	steerReplyWaitForTest = 200 * time.Millisecond
	t.Cleanup(func() { steerReplyWaitForTest = old })
	calls := 0
	LaunchWeb = func(string, steer.Command) int { calls++; return 0 }
	t.Cleanup(func() { LaunchWeb = nil })
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + ":1"}, &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "did not start its web page") || calls != 0 {
		t.Fatalf("exit=%d stderr=%q calls=%d", code, errb.String(), calls)
	}
}
```

```go
// cmd/gg/webflags_test.go
package main

import (
	"reflect"
	"testing"
)

func TestExtractWebFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantWeb  bool
		wantAddr string
		wantRest []string
	}{
		{"absent", []string{"status"}, false, "", []string{"status"}},
		{"web", []string{"--web"}, true, "", []string{}},
		{"addr space form", []string{"--web-addr", "127.0.0.1:7777"}, true, "127.0.0.1:7777", []string{}},
		{"addr equals form", []string{"--web-addr=127.0.0.1:7777", "--record", "x"}, true, "127.0.0.1:7777", []string{"--record", "x"}},
		{"addr implies web", []string{"--web-addr", "127.0.0.1:1"}, true, "127.0.0.1:1", []string{}},
		{"no value is dropped safely", []string{"--web-addr"}, false, "", []string{}},
		{"gg web keeps its own flags", []string{"web", "--addr", "127.0.0.1:2"}, false, "", []string{"web", "--addr", "127.0.0.1:2"}},
	}
	for _, tt := range tests {
		web, addr, rest := extractWebFlags(tt.args)
		if web != tt.wantWeb || addr != tt.wantAddr || !reflect.DeepEqual(rest, tt.wantRest) {
			t.Errorf("%s: got (%v,%q,%v) want (%v,%q,%v)", tt.name, web, addr, rest, tt.wantWeb, tt.wantAddr, tt.wantRest)
		}
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run TestSteerServe 2>&1 | tail -4; go test ./internal/cli/ -run 'TestOpenWebAsks|TestOpenWebServeTimeout' 2>&1 | tail -4; go test ./cmd/gg/ -run TestExtractWebFlags 2>&1 | tail -3`
Expected: TUI: `unknown command "serve"` reply → FAIL; CLI: `LaunchWeb` called / no timeout message → FAIL; cmd: `undefined: extractWebFlags`.

- [ ] **Step 3: Implement**

`internal/steer/steer.go:81` comment: add `| "serve"` with a note (TUI only: start serving its web page; the reply's Detail is the URL). `internal/tui/steer.go` `applySteer`: in the early `switch` (before `steerRefusal`, like `files` — a busy TUI can still start serving):

```go
	case c.Cmd == "serve":
		return m.steerServe(c)
```

```go
// steerServe (`gg open --web` with a live TUI): start the hosted page if
// needed and answer with its URL. Never refused for the user's state — it
// moves nothing on screen.
func (m Model) steerServe(c steer.Command) (Model, tea.Cmd) {
	m = m.ensureWeb()
	switch {
	case NewWebHost == nil:
		return m, m.answerSteer(c, steerFail(c, "this gg cannot serve a web page"))
	case m.web.host != nil:
		return m, m.answerSteer(c, steerOK(c, m.web.url))
	}
	m.web.pendingServe = append(m.web.pendingServe, c)
	if m.web.starting {
		return m, nil
	}
	m.web.starting = true
	return m, startWebCmd(m.svc, m.webAddr(), false)
}
```

`webHostState` gains `pendingServe []steer.Command`; `onWebStarted` answers each pending command (`steerOK(c, url)` or `steerFail(c, err.Error())`) via `m.answerSteer` and clears the slice (batch the reply cmds).

`internal/cli/open.go`: a helper and two call sites:

```go
// askTUIToServe (--web with a live TUI and no page): post "serve" to the
// TUI's inbox and wait for the URL it answers with. false = printed why.
func askTUIToServe(dir string, stderr io.Writer) (string, bool) {
	id, err := steer.Post(dir, steer.Command{Cmd: "serve", Wait: true})
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return "", false
	}
	rep, ok := steer.AwaitReply(dir, id, steerReplyWaitForTest)
	switch {
	case !ok:
		fmt.Fprintf(stderr, "open: the TUI in %s did not start its web page\n", dir)
		return "", false
	case !rep.OK:
		fmt.Fprintln(stderr, "open:", rep.Error)
		return "", false
	}
	return rep.Detail, true
}
```

`openWeb`: after the `if r := routeFor(dir); r.webOK {…}` block and before `if LaunchWeb == nil`:

```go
	if r := routeFor(dir); r.tuiOK && !r.webOK {
		url, ok := askTUIToServe(dir, stderr)
		if !ok {
			return 1
		}
		if c.ID == "" {
			c.ID = steer.NewID()
		}
		if err := postWebSteer(url, c); err != nil {
			fmt.Fprintln(stderr, "web:", err)
			return 1
		}
		fmt.Fprintln(stdout, "web: sent")
		fmt.Fprintln(stdout, "steered: "+res.Checkout)
		return 0
	}
```

`openWebBare`: same shape, printing `"web: "+url` and returning 0. Note `steerReplyWaitForTest` is 2 s; a cold host binds in milliseconds, so it stands (rename is out of scope).

`cmd/gg/main.go`: `extractWebFlags` beside `extractTimeTrack` (same three-arm switch for `--web-addr`, plus `case a == "--web": web = true`; `--web-addr` with a value sets `web = true` too; args after the subcommand position are still scanned — `gg web --addr` uses `--addr`, not `--web-addr`, so it is untouched). Call it where `--cwd-file` is extracted; pass into `launchTUI(dir, at, recordPath, cwdFile, webOn, webAddr)` → `tui.Run(svc, tui.RunOptions{RecordPath: recordPath, At: at, Web: webOn, WebAddr: webAddr})`. Beside `cli.LaunchWeb`:

```go
	// The TUI's own web page: the same server gg web runs, hosted in this
	// process so the browser and the terminal share agent sessions. Services
	// the hosted server opens itself take the TUI's ssh-batch runner.
	tui.NewWebHost = func(svc *domain.Service) tui.WebHost { return web.NewHost(svc, domain.OpenTUI, true) }
```

Also `cli.LaunchTUI`'s call passes zero web options (a `gg open` launch never serves unless config says so).

- [ ] **Step 4: Build and run the four packages**

Run: `go build ./... && go test ./internal/tui/ ./internal/cli/ ./internal/steer/ ./cmd/gg/ 2>&1 | tail -6`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/steer/steer.go internal/tui internal/cli cmd/gg
git commit -m "feat: gg open --web asks a live TUI to serve (inbox \"serve\"); gg --web / --web-addr; the host is wired in cmd/gg"
```

---

### Task 10: Browser check, docs, memory

**Files:**
- Create (scratch, job dir `/home/homeend/.claude/jobs/3683fa84/tmp/`): `runtui.sh`, `checkhosted.mjs`, `rununfixedtui.sh`
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md` (the `tui` and `web` rows), `docs/CLAUDE-details.md`, memory `agent-sessions-feature.md` + `MEMORY.md`, `config-settings-registry.md`

- [ ] **Step 1: Unfixed check on the installed `gg`**

```bash
cd /home/homeend/.claude/jobs/3683fa84/tmp && rm -rf unfixed-tui && mkdir -p unfixed-tui/repo unfixed-tui/xdg/state unfixed-tui/xdg/config
cd unfixed-tui/repo && git init -q && git commit -q --allow-empty -m init && echo a > a.txt && git add a.txt && git commit -q -m a
XDG_STATE_HOME=$PWD/../xdg/state XDG_CONFIG_HOME=$PWD/../xdg/config gg --web --web-addr 127.0.0.1:0 </dev/null 2>&1 | head -3
```

Expected: the installed `gg` treats `--web` as unknown (or falls into the TUI with the flag ignored and no `web.json` appears under `unfixed-tui/xdg/state`). Record the exact output in the ledger. Then `/mnt/t/others/gigagit/tui-capture.sh --gg $(which gg) --repo $PWD "C-p"` and assert the palette snapshot has no "Open in browser".

- [ ] **Step 2: Fixed check — TUI under tmux, session from the menu, URL from web.json, playwright**

`runtui.sh` (own tmux session name `claude-webhosted`, isolated XDG, port 0):

```bash
#!/bin/bash
set -u
T=/home/homeend/.claude/jobs/3683fa84/tmp; W=/mnt/t/others/gigagit/.claude/worktrees/web-hosted
R=$T/hosted/repo; X=$T/hosted/xdg
rm -rf $T/hosted; mkdir -p $R $X/state $X/config
cd $R && git init -q && git commit -q --allow-empty -m init && echo a > a.txt && git add a.txt && git commit -q -m a
(cd $W && go build -o $T/gg-hosted ./cmd/gg) || exit 1
printf '[console]\nshell = "sh"\n' > $X/config/gg/config.toml 2>/dev/null || { mkdir -p $X/config/gg; printf '[console]\nshell = "sh"\n' > $X/config/gg/config.toml; }
tmux kill-session -t claude-webhosted 2>/dev/null
tmux new-session -d -s claude-webhosted -x 140 -y 40 -c $R "XDG_STATE_HOME=$X/state XDG_CONFIG_HOME=$X/config BROWSER=true $T/gg-hosted --web --web-addr 127.0.0.1:0"
for i in $(seq 1 60); do WJ=$(find $X/state -name web.json 2>/dev/null | head -1); [ -n "$WJ" ] && break; sleep 0.5; done
URL=$(python3 -c "import json,sys;print(json.load(open('$WJ'))['url'])"); echo "URL=$URL"
# Worktrees panel → . menu → Open terminal (read the menu with tui-capture first to get the exact keys; the row label is "Open terminal in <wt>")
tmux send-keys -t claude-webhosted "2" ; sleep 0.5   # the Worktrees tab key — verify in a capture
tmux send-keys -t claude-webhosted "." ; sleep 0.5
tmux send-keys -t claude-webhosted "/Open terminal" Enter; sleep 1.5
tmux send-keys -t claude-webhosted C-]; sleep 0.3
cd $T && node checkhosted.mjs "$URL" $T/hosted.png
tmux send-keys -t claude-webhosted C-] ; tmux send-keys -t claude-webhosted "q"; sleep 0.5; tmux send-keys -t claude-webhosted Enter  # confirm "Kill all and quit"
sleep 3; node checkdown.mjs "$URL"
tmux kill-session -t claude-webhosted 2>/dev/null
```

`checkhosted.mjs` (playwright from the old scratchpad `node_modules` symlink; the visibility helper from `checkattach.mjs`): assert (1) `/api/repo` JSON has `hosted: true`; (2) ctrl+\ opens the switcher with the Agents tab listing a `sh` row (visible); (3) enter shows `#console` with `$` in `#console-grid` (visible); (4) the palette (`:` or its key — read palette.js) has no "switch repo…" and no "open repo (path)…" rows; (5) the worktree row's context menu has no "switch here". `checkdown.mjs`: after the TUI quit, load the page and assert `#server-down` is visible within 10 s (or the events stream received `shutdown`). Record PASS/FAIL lines and the screenshot in the ledger.

- [ ] **Step 3: Docs**

`CHANGELOG.md` (top entry): "The TUI serves its own web page: Open in browser in the command palette, `[web] serve`/`addr`, `gg --web`/`--web-addr`; the page follows the TUI's repo and hides its own switching; `gg open --web` asks a live TUI to serve; agent consoles in the browser now show the terminal's sessions (one session manager per process); change signals are fanned out to every listener." `README.md`: under the web section and the agent-consoles section, the palette command, the setting, the flags, the `gg open --web` behaviour. `CLAUDE.md`: `tui` row add "serves the gg web page in-process (`WebHost` seam, `webhost.go`)"; `web` row add "reusable `Host` (in-process by the TUI, standalone by `gg web`)"; `agentsession` row: "`Subscribe` broadcaster (per-subscriber coalescing)". `docs/CLAUDE-details.md`: a section "### The TUI serves its own web page (2026-09-28)" — spec path, the Host lifecycle, `adoptService`, the hosted rules, the broadcaster and the subscription-lifetime rule (every `m.console = nil` goes through `dropConsole`), the `serve` inbox command, the `gg open --web` routing table row, the browser-check recipe. Memory: `agent-sessions-feature.md` (status line: MERGED sha when merged; rulings 1–7), `MEMORY.md` line, `config-settings-registry.md` (+ `[web] serve`, `[web] addr`).

- [ ] **Step 4: Gates**

Run: `cd .claude/worktrees/web-hosted && ./test.sh 2>&1 | tail -3` then `./test.sh race 2>&1 | tail -3` (do not edit files while either runs; the wrapper may exit 1 with an "all green" last line — trust the script's own last line).
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md
git commit -m "docs: the TUI serves its own web page — changelog, readme, package map, details"
```

Then ask before merging (`merge --no-ff` with a custom message; CHANGELOG/style.css conflicts keep both sides), build + tests on the merged tree, `./build.sh install` + `./build.sh web`, remove the worktree and branch, update memory.
