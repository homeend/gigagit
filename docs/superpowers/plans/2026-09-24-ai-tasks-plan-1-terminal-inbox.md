# AI tasks — Plan 1: worktree terminal + GG_INBOX discovery

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans
> (this repo forbids subagents) to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Worktrees `.` → *Open terminal* starts a shell session; every
session gg starts carries `GG_INBOX`, so `gg session …` from inside it reaches
the gg that started it — and a worktree-bound command from another worktree
raises a notice instead of landing on the wrong checkout.

**Architecture:** `domain` picks the shell (`terminalShell`) and starts it
(`StartTerminal`); `StartSession` gains an `env` parameter. `steer.Command`
gains `Worktree` (wire) and `From` (set by `Drain`, never on the wire). The
CLI stamps `Worktree` and prefers a live `$GG_INBOX`. The TUI passes
`GG_INBOX=<its inbox>` to every child, keeps presence in (and drains) every
inbox a running child was given, answers each command in the inbox it came
from, and turns a worktree mismatch into a notice (`steerSwitchAsk`).

**Tech Stack:** Go 1.26, Bubble Tea v1, internal/steer, internal/agentsession.

**Spec:** `docs/superpowers/specs/2026-09-24-ai-tasks-in-sessions-design.md`
(rulings 7 and 9, Architecture §A).

## Global Constraints

- Every user-visible TUI string goes through `i18n.T` with a literal key in
  all four bundles (ja/ko/zh/ru) — the AST gates fail otherwise.
- `internal/tui` and `internal/cli` never import `internal/git`.
- Engine/CLI/steer reply prose stays English.
- New tests call `t.Parallel()` unless they set env (`t.Setenv`) or global seams.
- No subagents. Worktree `.claude/worktrees/ai-tasks`, branch `feat/ai-tasks`.
- `./test.sh race` green before asking to merge.

## Review Focus

1. A command from a child whose inbox is NOT gg's current one must be
   answered in the child's inbox (`From`), or the CLI waits 2 s and prints
   "queued".
2. A kept inbox that another live gg owns (a second TUI on that worktree) must
   NOT be touched or drained — check the presence PID first.
3. `reRoot` must not delete presence from an inbox a running child still
   holds (the ≤1 s gap would make `gg session status` fail).
4. A navigate with no `Worktree` (an older CLI, the `--at` landing, the `#`
   prompt) must apply exactly as today.
5. Windows shell pick: `pwsh` → `powershell` → `cmd`; a configured
   `[console] shell` wins even when not found on PATH (the start error says so).

---

### Task 1: `[console] shell` config key

**Files:**
- Modify: `internal/config/config.go` (ConsoleConfig, overlayConsole)
- Modify: `internal/config/template.go:85-86` (settingDocs row)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.ConsoleConfig.Shell string` (`toml:"shell"`), default `""`.

- [ ] **Step 1: Write the failing test**

```go
func TestConsoleShellOverlay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	if err := os.WriteFile(global, []byte("[console]\nshell = \"/bin/zsh\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(global, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Console.Shell != "/bin/zsh" {
		t.Fatalf("shell = %q", cfg.Console.Shell)
	}
	if def := Defaults().Console.Shell; def != "" {
		t.Fatalf("default shell = %q, want empty (auto-detect)", def)
	}
}
```

(If the package's loader/defaults helpers are named differently, use the ones
the existing `TestConsole…` tests use — ledger a ruling.)

- [ ] **Step 2: Run it** — `go test ./internal/config -run TestConsoleShellOverlay`
  Expected: FAIL (`cfg.Console.Shell undefined`).

- [ ] **Step 3: Implement**

```go
type ConsoleConfig struct {
	StepOutKey  string `toml:"step_out_key"`
	SessionsKey string `toml:"sessions_key"`
	Shell       string `toml:"shell"` // Open terminal's shell; "" = $SHELL / pwsh→powershell→cmd
}
```

In `overlayConsole` add `if src.Shell != "" { dst.Shell = src.Shell }`. In
`settingDocs` after `sessions_key`:

```go
{"console", "shell", "", "Open terminal (Worktrees . menu): the shell to run; empty = $SHELL (else sh) on Unix, pwsh → powershell → cmd on Windows"},
```

- [ ] **Step 4: Run** `go test ./internal/config` — Expected: PASS (the
  template/settingDocs coverage tests included).

- [ ] **Step 5: Commit** `feat(config): [console] shell for Open terminal`

---

### Task 2: domain — shell choice, `StartTerminal`, `StartSession(env)`

**Files:**
- Modify: `internal/domain/sessions.go`
- Modify callers: `internal/tui/agent_start_popup.go:146`,
  `internal/tui/console_test.go:32`, `internal/domain/sessions_test.go:106,205`
- Test: `internal/domain/sessions_test.go`

**Interfaces:**
- Produces:
  - `func terminalShell(goos string, getenv func(string) string, lookPath func(string) (string, error), override string) []string`
  - `func (s *Service) StartTerminal(ctx context.Context, shell, worktreeDir string, cols, rows int, env []string) (*AgentSession, error)` (label `"Terminal"`)
  - `StartSession(ctx, tc, worktreeDir, cols, rows int, env []string)` — `env` appended to `StartSpec.Env`.

- [ ] **Step 1: Write the failing tests**

```go
func TestTerminalShell(t *testing.T) {
	t.Parallel()
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	have := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range names {
				if h == n {
					return `C:\bin\` + n + ".exe", nil
				}
			}
			return "", errors.New("not found")
		}
	}
	for _, c := range []struct {
		name, goos, override string
		env                  map[string]string
		path                 []string
		want                 string
	}{
		{"unix SHELL", "linux", "", map[string]string{"SHELL": "/bin/zsh"}, nil, "/bin/zsh"},
		{"unix fallback", "linux", "", nil, nil, "/bin/sh"},
		{"override wins", "linux", "/usr/bin/fish", map[string]string{"SHELL": "/bin/zsh"}, nil, "/usr/bin/fish"},
		{"win pwsh", "windows", "", nil, []string{"pwsh", "powershell", "cmd"}, `C:\bin\pwsh.exe`},
		{"win powershell", "windows", "", nil, []string{"powershell", "cmd"}, `C:\bin\powershell.exe`},
		{"win cmd via COMSPEC", "windows", "", map[string]string{"COMSPEC": `C:\Windows\system32\cmd.exe`}, nil, `C:\Windows\system32\cmd.exe`},
		{"win override", "windows", "nu", nil, []string{"pwsh"}, "nu"},
	} {
		if got := terminalShell(c.goos, env(c.env), have(c.path...), c.override); len(got) != 1 || got[0] != c.want {
			t.Errorf("%s: %v, want [%s]", c.name, got, c.want)
		}
	}
}

func TestStartSessionPassesEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { Sessions().KillAll(context.Background()); restore() })
	dir := t.TempDir()
	svc := Open(dir)
	tc := config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: `printf "INBOX=%s" "$GG_INBOX"; sleep 5`}
	s, err := svc.StartSession(context.Background(), tc, dir, 80, 10, []string{"GG_INBOX=/tmp/inbox-x"})
	if err != nil {
		t.Fatal(err)
	}
	waitSessionText(t, s, "INBOX=/tmp/inbox-x")
}

func TestStartTerminalRunsTheShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { Sessions().KillAll(context.Background()); restore() })
	dir := t.TempDir()
	s, err := Open(dir).StartTerminal(context.Background(), "/bin/sh", dir, 80, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Info().Label; got != "Terminal" {
		t.Fatalf("label = %q", got)
	}
	s.SendText("echo TERM-OK\r")
	waitSessionText(t, s, "TERM-OK")
}
```

`waitSessionText` — add beside the existing session tests if absent: poll
`s.Screen().Lines` for 5 s, `t.Fatalf` otherwise. `Open(dir)` on a non-repo
temp dir must work for these (the existing test at line 106 shows how the
service is built — reuse that construction; ledger if it differs).

- [ ] **Step 2: Run** `go test ./internal/domain -run 'TestTerminalShell|TestStartSessionPassesEnv|TestStartTerminal'`
  Expected: FAIL (undefined / too many arguments).

- [ ] **Step 3: Implement**

```go
// terminalShell picks Open terminal's shell: the configured override first
// (run as given — a missing program fails at start with its own error),
// then $SHELL (else /bin/sh) on Unix, and pwsh → powershell → %COMSPEC%
// (else cmd.exe) on Windows.
func terminalShell(goos string, getenv func(string) string, lookPath func(string) (string, error), override string) []string {
	if override != "" {
		return []string{override}
	}
	if goos != "windows" {
		if sh := getenv("SHELL"); sh != "" {
			return []string{sh}
		}
		return []string{"/bin/sh"}
	}
	for _, name := range []string{"pwsh", "powershell"} {
		if p, err := lookPath(name); err == nil {
			return []string{p}
		}
	}
	if cs := getenv("COMSPEC"); cs != "" {
		return []string{cs}
	}
	return []string{"cmd.exe"}
}

// StartTerminal runs an interactive shell in worktreeDir as a session
// labelled "Terminal" — argv directly, no `-c` wrapper: the shell IS the
// program. env carries GG_INBOX (and anything else the frontend adds).
func (s *Service) StartTerminal(ctx context.Context, shell, worktreeDir string, cols, rows int, env []string) (*AgentSession, error) {
	repo, err := s.RepoName(ctx)
	if err != nil || repo == "" {
		repo = filepath.Base(worktreeDir)
	}
	argv := terminalShell(runtime.GOOS, os.Getenv, exec.LookPath, shell)
	return Sessions().Start(agentsession.StartSpec{
		Label: "Terminal", Repo: repo, Dir: worktreeDir, Argv: argv, Env: env,
		Cols: cols, Rows: rows,
		TracePath: sessionTracePath(os.Getenv("GG_SESSION_TRACE"), "Terminal", time.Now()),
	})
}
```

`StartSession` gets the trailing `env []string` and sets `Env: env` in its
`StartSpec`. Update the four callers (pass `nil` in tests; the TUI caller is
changed properly in Task 5 — pass `nil` for now).

- [ ] **Step 4: Run** `go test ./internal/domain ./internal/tui -run 'Session|Terminal|Console|Agent'`
  Expected: PASS.

- [ ] **Step 5: Commit** `feat(domain): StartTerminal + shell choice; StartSession takes env`

---

### Task 3: steer — `Command.Worktree` and `Command.From`

**Files:**
- Modify: `internal/steer/steer.go` (Command, Drain)
- Test: `internal/steer/steer_test.go`

**Interfaces:**
- Produces: `Command.Worktree string \`json:"worktree,omitempty"\`` (the
  sender's checkout); `Command.From string \`json:"-"\`` (the inbox `Drain`
  read it from).

- [ ] **Step 1: Write the failing test**

```go
func TestDrainStampsFromAndKeepsWorktree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Post(dir, Command{Cmd: "reload", Worktree: "/w/b"}); err != nil {
		t.Fatal(err)
	}
	got := Drain(dir)
	if len(got) != 1 || got[0].From != dir || got[0].Worktree != "/w/b" {
		t.Fatalf("drained %+v", got)
	}
	data, _ := json.Marshal(got[0])
	if strings.Contains(string(data), dir) {
		t.Fatalf("From must never reach the wire: %s", data)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/steer -run TestDrainStamps` — Expected: FAIL (unknown fields).

- [ ] **Step 3: Implement** — add both fields to `Command` (document:
  Worktree = "the checkout the sender runs in; a consumer showing another
  worktree must not apply a worktree-bound command to its own"; From = "set by
  Drain; answers go back to this inbox"). In `Drain`, before `append`:
  `c.From = dir`.

- [ ] **Step 4: Run** `go test ./internal/steer` — Expected: PASS.

- [ ] **Step 5: Commit** `feat(steer): commands carry the sender's worktree; Drain records the inbox`

---

### Task 4: CLI — prefer a live `$GG_INBOX`, stamp `Worktree`

**Files:**
- Modify: `internal/cli/session.go` (`sendSteer`, the callers that build commands, `cmdSession`)
- Modify: `internal/cli/open.go:107` (pass the link's checkout as worktree)
- Test: `internal/cli/session_inbox_test.go` (new)

**Interfaces:**
- Consumes: `steer.Command.Worktree`.
- Produces: `var sessionGetenv = os.Getenv` (test seam);
  `func preferredInbox(dir string) string` — `$GG_INBOX` when a TUI or web
  presence there is live, else `dir`.

- [ ] **Step 1: Write the failing tests**

```go
func TestPreferredInboxUsesLiveGGInbox(t *testing.T) {
	own, cwd := t.TempDir(), t.TempDir()
	if err := steer.Touch(own, steer.TUIPresence, steer.Presence{PID: 1, Worktree: "/w/a"}); err != nil {
		t.Fatal(err)
	}
	old := sessionGetenv
	sessionGetenv = func(k string) string {
		if k == "GG_INBOX" {
			return own
		}
		return ""
	}
	t.Cleanup(func() { sessionGetenv = old })
	if got := preferredInbox(cwd); got != own {
		t.Fatalf("got %s, want the live GG_INBOX %s", got, own)
	}
}

func TestPreferredInboxFallsBackWhenNotLive(t *testing.T) {
	stale, cwd := t.TempDir(), t.TempDir()
	old := sessionGetenv
	sessionGetenv = func(k string) string {
		if k == "GG_INBOX" {
			return stale
		}
		return ""
	}
	t.Cleanup(func() { sessionGetenv = old })
	if got := preferredInbox(cwd); got != cwd {
		t.Fatalf("got %s, want the cwd inbox", got)
	}
}

func TestSendSteerStampsWorktree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{PID: 1}); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := sendSteerFrom(dir, "/w/b", steer.Command{Cmd: "reload"}, true, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	got := steer.Drain(dir)
	if len(got) != 1 || got[0].Worktree != "/w/b" {
		t.Fatalf("drained %+v", got)
	}
}
```

The two env tests mutate a package var: no `t.Parallel()`.

- [ ] **Step 2: Run** `go test ./internal/cli -run 'PreferredInbox|SendSteerStamps'` — Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// sessionGetenv reads GG_INBOX; a variable so tests stay off the process env.
var sessionGetenv = os.Getenv

// preferredInbox is the inbox a session verb talks to: the one the gg that
// started this process named in $GG_INBOX, while a TUI or web page there is
// live — so an agent in worktree B reaches the gg showing A that launched it
// — else dir (this worktree's, or a link's checkout's).
func preferredInbox(dir string) string {
	if own := sessionGetenv("GG_INBOX"); own != "" {
		if _, ok := steer.Live(own, steer.TUIPresence); ok {
			return own
		}
		if _, ok := steer.Live(own, steer.WebPresence); ok {
			return own
		}
	}
	return dir
}
```

Rename the body of `sendSteer` to `sendSteerFrom(dir, worktree string, c
steer.Command, noWait bool, stdout, stderr io.Writer) int`, which sets
`c.Worktree = worktree` when empty and `dir = preferredInbox(dir)` first.
Every existing caller passes the worktree it knows: flag-built commands the
cwd top-level (thread it from `cmdSession`: compute `top` once next to
`sessionInboxDir` and hand it to `runSession` → the subcommand funcs; tests
calling `runSession` pass `""`), link-built ones the link's `res.Checkout`
(`linkSteerDir` already resolves it; return it alongside), `gg open` (open.go)
likewise. `sessionStatus` reads presence via `preferredInbox(dir)` too.

- [ ] **Step 4: Run** `go test ./internal/cli` — Expected: PASS (all existing session/link tests unchanged).

- [ ] **Step 5: Commit** `feat(cli): gg session prefers a live $GG_INBOX and names its worktree`

---

### Task 5: TUI — `GG_INBOX` for children, Open terminal, answers to `From`

**Files:**
- Modify: `internal/tui/agent_start_popup.go` (start → env; `agentStartedMsg` gains `inbox`; `sessionMenuRows` gains Open terminal)
- Modify: `internal/tui/steer.go` (`answerSteer` reply dir)
- Create: `internal/tui/terminal.go` (`openTerminal`)
- Modify: i18n bundles ×4; `internal/tui/help.go` (help row)
- Test: `internal/tui/terminal_test.go`, `internal/tui/steer_test.go`

**Interfaces:**
- Consumes: `svc.StartTerminal`, `svc.StartSession(…, env)`, `steer.Command.From`.
- Produces: `func (m Model) childEnv() []string` (`["GG_INBOX=<m.steerDir>"]`
  when `steerActive()`, else nil); `m.childInbox map[domain.SessionID]string`
  (a map field — survives the value copy — filled in `applyAgentStarted`).

- [ ] **Step 1: Write the failing tests**

```go
func TestOpenTerminalRowStartsAFocusedShell(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 0
	var row *actionRow
	for _, r := range m.sessionMenuRows() {
		if r.id == "open-terminal" {
			row = &r
		}
	}
	if row == nil {
		t.Fatal("no Open terminal row on a worktree")
	}
	mm, cmd := row.run(m)
	m = mm.(Model)
	m = drainMsgs(t, m, cmd) // the package's helper that feeds cmd results back through Update
	if m.console == nil || !m.console.focused {
		t.Fatalf("terminal console must open focused: %+v", m.console)
	}
	s, _ := m.consoleSession()
	if s.Info().Label != "Terminal" {
		t.Fatalf("label %q", s.Info().Label)
	}
}

func TestChildEnvNamesTheInbox(t *testing.T) {
	m := loadedModel(t)
	m.steerDir = t.TempDir()
	if got := m.childEnv(); len(got) != 1 || got[0] != "GG_INBOX="+m.steerDir {
		t.Fatalf("childEnv = %v", got)
	}
}

func TestSteerReplyGoesToTheCommandsInbox(t *testing.T) {
	m := loadedModel(t)
	m.steerDir = t.TempDir()
	other := t.TempDir()
	c := steer.Command{ID: "x1", Cmd: "focus", Panel: "branches", Wait: true, From: other}
	m, cmd := m.applySteer(c)
	runCmd(cmd) // execute the reply write
	if _, ok := steer.AwaitReply(other, "x1", time.Second); !ok {
		t.Fatal("the reply must land in the inbox the command came from")
	}
}
```

Use the helpers this package's tests already use for draining commands and
enabling steering (see `steer_test.go`); adapt names and ledger a ruling.

- [ ] **Step 2: Run** `go test ./internal/tui -run 'OpenTerminal|ChildEnv|SteerReplyGoesTo'` — Expected: FAIL.

- [ ] **Step 3: Implement**
  - `answerSteer`: `dir := m.steerDir; if c.From != "" { dir = c.From }`, and
    the `!c.Wait || m.steerDir == ""` guard becomes `!c.Wait || dir == ""`.
  - `childEnv()` as above; `agentStartPopup.start` passes `m.childEnv()` to
    `StartSession` and puts `inbox: m.steerDir` on `agentStartedMsg`;
    `applyAgentStarted` records `m.childInbox[msg.id] = msg.inbox` when
    non-empty (initialise the map in the Model constructor).
  - `terminal.go`: `openTerminal(worktree string) (Model, tea.Cmd)` — status
    `starting a terminal…`, off-thread `svc.StartTerminal(ctx,
    m.cfg.Console.Shell, worktree, cols, rows, m.childEnv())`, returning
    `agentStartedMsg{id, name: "Terminal", inbox}` so the console opens
    through the same `applyAgentStarted` path.
  - `sessionMenuRows`: on a worktree row return
    `[start-agent, open-terminal]`, label `i18n.T("Open terminal")`.
  - i18n: `"Open terminal"`, `"starting a terminal…"` and the help row
    `"Open terminal (.-menu on a Worktrees row): an interactive shell in that worktree, in the agent console ([console] shell)"` in ja/ko/zh/ru.
  - Update `TestSessionMenuRows` expectation `start-agent` →
    `start-agent,open-terminal`.

- [ ] **Step 4: Run** `go test ./internal/tui ./internal/i18n` (long; run in
  the background, read the tail) — Expected: PASS.

- [ ] **Step 5: Commit** `feat(tui): Open terminal; children get GG_INBOX; replies go to the command's inbox`

---

### Task 6: TUI — keep inboxes a running child holds

**Files:**
- Create: `internal/tui/steer_kept.go`
- Modify: `internal/tui/steer.go` (`closeSteerInbox` spares kept dirs; `drainSteer` drains kept dirs)
- Modify: `internal/tui/model.go` (heartbeat: `m = m.tendKeptInboxes()`), `internal/tui/run.go` (exit: remove kept presences)
- Test: `internal/tui/steer_kept_test.go`

**Interfaces:**
- Consumes: `m.childInbox`.
- Produces: `func (m Model) keptInboxes() []string` — the distinct inboxes of
  RUNNING children, minus `m.steerDir`, sorted;
  `func (m Model) tendKeptInboxes() Model` — for each kept dir whose presence
  is absent or ours (PID == os.Getpid()): `steer.Touch` it; forget
  `childInbox` entries whose session is gone or exited and `steer.Remove`
  presence from dirs no longer kept (only when ours); `m.keptSteer
  map[string]bool` records what we currently claim.

- [ ] **Step 1: Write the failing tests**

```go
func TestKeptInboxSurvivesReRootAndIsDrained(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 5`)
	a := t.TempDir()
	m.steerDir = a
	m.childInbox[s.Info().ID] = a
	m.steerDir = t.TempDir() // the TUI moved on (what reRoot's re-home does)
	m = m.tendKeptInboxes()
	if _, ok := steer.Live(a, steer.TUIPresence); !ok {
		t.Fatal("a running child's inbox must keep a live presence")
	}
	if _, err := steer.Post(a, steer.Command{Cmd: "focus", Panel: "branches"}); err != nil {
		t.Fatal(err)
	}
	m, _ = m.drainSteer()
	if left := steer.Drain(a); len(left) != 0 {
		t.Fatal("a kept inbox must be drained")
	}
}

func TestKeptInboxReleasedWhenChildEnds(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `exit 0`)
	a := t.TempDir()
	m.childInbox[s.Info().ID] = a
	m.steerDir = t.TempDir()
	m = m.tendKeptInboxes()
	<-s.Done()
	m = m.tendKeptInboxes()
	if _, ok := steer.Live(a, steer.TUIPresence); ok {
		t.Fatal("the presence must go when the last child holding the inbox ends")
	}
}

func TestKeptInboxNotStolenFromAnotherGG(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 5`)
	a := t.TempDir()
	if err := steer.Touch(a, steer.TUIPresence, steer.Presence{PID: os.Getpid() + 1}); err != nil {
		t.Fatal(err)
	}
	m.childInbox[s.Info().ID] = a
	m.steerDir = t.TempDir()
	m = m.tendKeptInboxes()
	if p, _ := steer.Live(a, steer.TUIPresence); p.PID == os.Getpid() {
		t.Fatal("another live gg's presence must not be overwritten")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/tui -run KeptInbox` — Expected: FAIL.

- [ ] **Step 3: Implement** `steer_kept.go` per the Interfaces block (touch
  presence with `Worktree: m.snapshotWorktree` — the command is applied to
  what gg shows). `drainSteer`: after draining `m.steerDir`, loop over
  `m.keptSteer` dirs and apply their commands the same way.
  `closeSteerInbox`: skip the `steer.Remove` when `m.steerDir` is among
  `keptInboxes()` (the child still points there) and add it to
  `m.keptSteer`. Heartbeat (model.go, after `touchSteerPresence`):
  `m = m.tendKeptInboxes()`. Exit (run.go, beside `closeSteerInbox`): remove
  presence from every `m.keptSteer` dir we own.

- [ ] **Step 4: Run** `go test ./internal/tui -run 'KeptInbox|Steer'` — Expected: PASS.

- [ ] **Step 5: Commit** `feat(tui): keep steering presence in inboxes running children hold`

---

### Task 7: TUI — worktree mismatch asks via a notice

**Files:**
- Create: `internal/tui/steer_switch_ask.go`
- Modify: `internal/tui/steer.go` (`applySteer` gate), `internal/tui/notify.go` (`rebuildNotices` renders the ask), `internal/tui/steer_nav.go` (`consumeStartAt` replays a parked command), `internal/tui/model.go` (fields; reRoot clears the ask)
- i18n bundles ×4
- Test: `internal/tui/steer_switch_ask_test.go`

**Interfaces:**
- Consumes: `steer.Command.Worktree`, `guardedReRoot`, the notice centre.
- Produces:
  - `func steerWorktreeBound(c steer.Command) bool` — `navigate` with
    `File != ""` or `Target != nil`, and `highlight`.
  - `m.steerAsk *steerSwitchAsk{cmd steer.Command; from string}` — ONE slot,
    the latest request wins.
  - `m.startAtCmd *steer.Command` — replayed by `consumeStartAt` instead of
    `m.startAt` when set.

- [ ] **Step 1: Write the failing tests**

```go
func TestMismatchedNavigateAsksInsteadOfLanding(t *testing.T) {
	m := loadedModel(t)
	m.steerDir = t.TempDir()
	m.snapshotWorktree = "/w/a"
	c := steer.Command{ID: "n1", Cmd: "navigate", File: "x.go", Wait: true, Worktree: "/w/b", From: m.steerDir}
	m, cmd := m.applySteer(c)
	runCmd(cmd)
	rep, ok := steer.AwaitReply(m.steerDir, "n1", time.Second)
	if !ok || rep.OK || !strings.Contains(rep.Error, "asked the user to switch to /w/b") {
		t.Fatalf("reply = %+v", rep)
	}
	if m.steerAsk == nil || !m.noticesUnread {
		t.Fatal("a notice must ask the user")
	}
	found := false
	for _, n := range m.notices {
		if n.id == "steer_switch" {
			found = true
		}
	}
	if !found {
		t.Fatal("steer_switch notice missing")
	}
}

func TestMatchingOrUnboundCommandsApplyAsToday(t *testing.T) {
	m := loadedModel(t)
	m.steerDir = t.TempDir()
	m.snapshotWorktree = "/w/a"
	for _, c := range []steer.Command{
		{ID: "f1", Cmd: "focus", Panel: "branches", Worktree: "/w/b"},
		{ID: "n2", Cmd: "navigate", File: "x.go", Worktree: "/w/a/"},
		{ID: "n3", Cmd: "navigate", File: "x.go"}, // no Worktree: an older CLI / --at
	} {
		m2, _ := m.applySteer(c)
		if m2.steerAsk != nil {
			t.Fatalf("%s must not ask", c.ID)
		}
	}
}

func TestAcceptingTheAskSwitchesAndReplays(t *testing.T) {
	m := loadedModel(t)
	other := t.TempDir() // a real repo dir via the package's repo helper
	m.snapshotWorktree = "/w/a"
	m.steerAsk = &steerSwitchAsk{cmd: steer.Command{ID: "n1", Cmd: "navigate", File: "x.go", Worktree: other}, from: "Claude"}
	m = m.rebuildNotices()
	act := noticeActionByLabel(t, m, "steer_switch", i18n.T("Switch to %s and show", filepath.Base(other)))
	m2, _ := act.run(m)
	if m2.startAtCmd == nil || !m2.startAtPending {
		t.Fatal("accepting must arm the replay")
	}
	if m2.startAtCmd.Wait {
		t.Fatal("the replay must not wait for a reply nobody reads")
	}
}
```

(`noticeActionByLabel`: a small test helper finding the action on the notice
with that id. For `other`, create a real repo with the package's existing
repo helper so `guardedReRoot`'s check passes.)

- [ ] **Step 2: Run** `go test ./internal/tui -run 'Mismatched|MatchingOrUnbound|AcceptingTheAsk'` — Expected: FAIL.

- [ ] **Step 3: Implement**
  - `applySteer`, right after the two refusals:
    ```go
    if c.Worktree != "" && steerWorktreeBound(c) && !samePathTUI(c.Worktree, m.snapshotWorktree) {
    	return m.askSteerSwitch(c)
    }
    ```
  - `askSteerSwitch`: store `m.steerAsk = &steerSwitchAsk{cmd: c, from: steerSender(c)}`
    (`steerSender` = the label of a running session whose `childInbox` is
    `c.From` and whose Dir is under `c.Worktree`, else `i18n.T("an agent")`),
    `m = m.rebuildNotices()`, arm the blink exactly like `applyDriftReport`,
    and answer `steerFail(c, fmt.Sprintf("gg is showing worktree %s; asked the user to switch to %s", m.snapshotWorktree, c.Worktree))`.
  - `rebuildNotices`: append `steerAskNotice(m.steerAsk)` when non-nil — id
    `steer_switch`, title `i18n.T("%s wants to show %s in %s", from, where, base)`
    (`where` = `File:Line` or the commit sha7), actions
    `{Switch to <base> and show → run}`, `{Ignore → run clears m.steerAsk}`.
  - Switch action: `nm, cmd := m.guardedReRoot(ask.cmd.Worktree, false)`;
    on success set `c := ask.cmd; c.Wait = false; c.Worktree = ""; m.startAtCmd = &c;
    m.startAtPending, m.startAtPreviewsSeen = true, true`; clear `m.steerAsk`.
    Only `navigate` is replayed; for `highlight` the switch alone happens.
  - `consumeStartAt`: `if m.startAtCmd != nil { c := *m.startAtCmd; m.startAtCmd = nil; return m, func() tea.Msg { return startAtMsg{cmd: c} } }` before the link path.
  - `reRoot` clears nothing of `steerAsk` itself (the switch action already
    did); a reRoot by any other path clears `m.steerAsk` (the question is moot).
  - i18n (×4): `"%s wants to show %s in %s"`, `"Switch to %s and show"`,
    `"Ignore"` (reuse if it exists), `"an agent"`.

- [ ] **Step 4: Run** `go test ./internal/tui -run 'Steer|Notice|Mismatched|Accepting|MatchingOrUnbound'` — Expected: PASS.

- [ ] **Step 5: Commit** `feat(tui): a worktree-bound command from another worktree asks via a notice`

---

### Task 8: docs, skill, gates

**Files:**
- Modify: `internal/agentskill/using-gg.md` (GG_INBOX + the mismatch reply), `internal/agentskill/agentskill.go` (`Version` 94 → 95)
- Modify: `CHANGELOG.md`, `README.md` (Open terminal; `[console] shell`), `docs/CLAUDE-details.md` (GG_INBOX, kept inboxes, `From`, the ask), memory `agent-sessions-feature.md`

- [ ] **Step 1:** using-gg: under `gg session`, add: "Inside a console gg started (agent or terminal), `$GG_INBOX` names that gg, and `gg session` uses it first. A navigate/highlight from a worktree gg is not showing is not applied: exit 1 `gg is showing worktree <a>; asked the user to switch to <b>` — tell the user, do not retry in a loop." Bump `Version`.
- [ ] **Step 2:** CHANGELOG `## AI tasks — plan 1: terminal + GG_INBOX` (Added: Open terminal, `[console] shell`, GG_INBOX, the switch notice). README rows. CLAUDE-details section.
- [ ] **Step 3:** `go test ./internal/agentskill ./internal/tui -run 'Skill|HelpFooter|I18n|Menu'` — Expected: PASS.
- [ ] **Step 4:** `./test.sh race` (background; read the tail) — Expected: EXIT=0.
- [ ] **Step 5: Commit** `docs: AI tasks plan 1 — terminal, GG_INBOX, the switch notice`
