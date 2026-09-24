# Agent Sessions — Plan 1: spike + session core + domain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo NEVER uses subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A process-global manager that runs interactive agents in PTYs with an in-memory terminal emulator, reachable from `internal/domain`, plus the `session` external-tool category and the first-run auto-configure — everything Plan 2 (TUI) needs, with no UI yet.

**Architecture:** New DAG-leaf package `internal/agentsession` (PTY via `charmbracelet/x/xpty`, emulator via `charmbracelet/x/vt`). `internal/domain` owns one lazily-created process-global `*agentsession.Manager` (the `repogate` registry precedent — survives every `reRoot`, which reopens the `Service`) and re-exports the types frontends need as aliases. `internal/exttool` gains `CatSession`/`ModeSession` built-ins; `internal/config` accepts them.

**Tech Stack:** Go 1.26, `github.com/charmbracelet/x/xpty v0.1.4`, `github.com/charmbracelet/x/vt` (pseudo-version pinned in Task 1), `golang.org/x/sys` (unix process-group kill, windows job object).

**Spec:** `docs/superpowers/specs/2026-09-24-agent-sessions-design.md`

**Plan 2 (TUI)** is written after this plan merges; it consumes exactly the `domain` API listed in Task 6's *Produces* block.

## Global Constraints

- Work only in the worktree `/mnt/t/others/gigagit/.claude/worktrees/agent-sessions` (branch `feat/agent-sessions`); `cd` there in EVERY shell command (the shell cwd resets to the main checkout).
- `internal/agentsession` imports: stdlib, `x/xpty`, `x/vt`, `ultraviolet` (vt's cell type), `golang.org/x/sys`. **No** gigagit package imports.
- Frontends (`tui`/`cli`/`mcp`/`web`) never import `internal/agentsession` (archtest, Task 6).
- Scrollback cap: **10 000 lines** per session.
- Env for every session: inherited + `GG_SESSION_ID=<id>` + `TERM=xterm-256color`.
- Kill: Unix SIGTERM to the process group, SIGKILL after **3 s**; Windows: terminate via the job object + close the ConPTY.
- Built-in `session` commands: safe = `<bin>`; yolo (`OptIn`) = claude `--dangerously-skip-permissions`, codex `--dangerously-bypass-approvals-and-sandbox`, junie `--brave`, antigravity `--dangerously-skip-permissions`; kimi has no yolo. Each flag is verified against the installed CLI's `--help` in Task 5; an unverifiable flag is dropped.
- Tests: real PTY + scripted `sh` children; `t.Parallel()` except where global state is touched. TDD: every test is watched failing first.
- Commit trailers on every commit:
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43`.

## Review Focus

1. **Child exits while the reader is mid-read (Linux returns EIO, not EOF, on a PTY master after the slave side closes)** — the session must flip to `Exited` with the real exit code, never hang or report an error state. Pinned in Task 2 (`TestSessionExitCode` treats EIO as EOF).
2. **An agent that never reads stdin and floods output** — the reader must never block on the UI; `Changed` coalesces to one pending signal. Pinned in Task 2 (`TestChangedCoalesces`).
3. **Emulator query responses (DA / cursor-position report) must reach the child** — Claude Code and other TUIs send `ESC[6n`/`ESC[c` at startup and stall waiting for the reply if the emulator's output pipe isn't pumped back into the PTY. Pinned in Task 2 (`TestEmulatorRepliesReachChild`).
4. **Kill of an agent that spawned children (shell → node → tools)** — kill must take down the whole process group, not just the shell. Pinned in Task 4 (`TestKillTakesProcessGroup`).
5. **An agent that stops reading stdin while the user types or pastes** — the emulator writes key bytes into a synchronous pipe under its own lock; `pumpIn` must drain into a queue so neither `pumpOut` nor the `SendKey` caller deadlocks. Pinned in Task 3 (`TestPasteToNonReaderDoesNotBlock`).
6. **`Remove` on a running session / double `Kill` / `Write` after exit** — refused or no-op, never a panic or a write to a closed PTY. Pinned in Task 4 (`TestLifecycleMisuse`).

---

### Task 0: Spike — does `xpty` + `x/vt` run Claude Code correctly? (THROWAWAY, not committed)

**Files:**
- Create (scratchpad only): `$SCRATCH/spike/main.go`, `$SCRATCH/spike/go.mod` where
  `SCRATCH=/tmp/claude-1000/-mnt-t-others-gigagit/940fe89f-a4bc-4f07-b30b-700e08f4b67e/scratchpad`

**Interfaces:** none (throwaway). Output is a findings note appended to the spec under `## Spike findings`.

- [ ] **Step 1: Write the harness**

```go
// $SCRATCH/spike/main.go — Bubble Tea v1 program hosting one PTY child.
package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
)

type tick struct{}

type model struct {
	emu  *vt.SafeEmulator
	pty  xpty.Pty
	w, h int
}

func (m model) Init() tea.Cmd { return tea.Tick(33*time.Millisecond, func(time.Time) tea.Msg { return tick{} }) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tick:
		return m, m.Init()
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height-1
		m.emu.Resize(m.w, m.h)
		_ = m.pty.Resize(m.w, m.h)
	case tea.KeyMsg:
		if msg.String() == "ctrl+]" {
			return m, tea.Quit
		}
		if k, ok := toUV(msg); ok {
			m.emu.SendKey(k)
		}
	}
	return m, nil
}

func (m model) View() string {
	return m.emu.Render() + "\n[spike] ctrl+] quits"
}

// toUV maps the handful of keys the spike needs; Task 3 of Plan 2 builds the real table.
func toUV(k tea.KeyMsg) (uv.KeyEvent, bool) {
	switch k.Type {
	case tea.KeyRunes:
		return uv.KeyPressEvent{Code: k.Runes[0], Text: string(k.Runes)}, true
	case tea.KeyEnter:
		return uv.KeyPressEvent{Code: uv.KeyEnter}, true
	case tea.KeyEsc:
		return uv.KeyPressEvent{Code: uv.KeyEscape}, true
	case tea.KeyBackspace:
		return uv.KeyPressEvent{Code: uv.KeyBackspace}, true
	case tea.KeyUp:
		return uv.KeyPressEvent{Code: uv.KeyUp}, true
	case tea.KeyDown:
		return uv.KeyPressEvent{Code: uv.KeyDown}, true
	case tea.KeyTab:
		return uv.KeyPressEvent{Code: uv.KeyTab}, true
	case tea.KeyCtrlC:
		return uv.KeyPressEvent{Code: 'c', Mod: uv.ModCtrl}, true
	}
	return nil, false
}

func main() {
	argv := strings.Fields(os.Args[1])
	p, err := xpty.NewPty(80, 24)
	if err != nil {
		panic(err)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true} // remove on Windows build
	if err := p.Start(cmd); err != nil {
		panic(err)
	}
	if up, ok := p.(*xpty.UnixPty); ok {
		_ = up.Slave().Close()
	}
	emu := vt.NewSafeEmulator(80, 24)
	go func() { _, _ = io.Copy(emu, p) }() // child → screen
	go func() { _, _ = io.Copy(p, emu) }() // emulator replies + encoded keys → child
	go func() { _ = xpty.WaitProcess(context.Background(), cmd) }()
	_, _ = tea.NewProgram(model{emu: emu, pty: p}, tea.WithAltScreen()).Run()
}
```

- [ ] **Step 2: Build and run on Linux/WSL**

```bash
cd $SCRATCH/spike && go mod init spike && go get github.com/charmbracelet/bubbletea@v1.3.10 github.com/charmbracelet/x/xpty@v0.1.4 github.com/charmbracelet/x/vt@latest && go build -o spike . && ./spike claude
```

If `SafeEmulator` lacks `SendKey`/`Render`, use `vt.NewEmulator` guarded by a `sync.Mutex` instead and note it. Check by hand, and record each as PASS/FAIL:
1. Claude's startup screen renders (no stall → proves query replies reach the child).
2. Colours (256/true colour) and box-drawing glyphs.
3. Typing a prompt, enter, the streamed answer renders; esc interrupts.
4. Resizing the terminal re-lays out Claude's UI.
5. Paste of a multi-line text (bracketed) arrives as one paste.
6. `ctrl+]` quits the harness; `pgrep -f claude` afterwards shows the child gone once the PTY closes (SIGHUP).
Also run `./spike bash` and `./spike "vim /tmp/x"` (alt-screen).

- [ ] **Step 3: Windows (ConPTY)**

Remove the `SysProcAttr` line, `GOOS=windows go build -o spike.exe .`, run `spike.exe claude` from Windows Terminal (via `/mnt/c/...` copy); repeat checks 1–5.

- [ ] **Step 4: Dependency-bump check against gg**

`x/vt` pulls `x/ansi v0.11.7` (gg is on `v0.10.1`) and newer `colorprofile`. In the worktree:

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go get github.com/charmbracelet/x/vt@<pseudo-version from step 2> github.com/charmbracelet/x/xpty@v0.1.4 && go build ./... && ./test.sh unit; git checkout go.mod go.sum
```

Expected: builds, unit tests green (record any failure — an `ansi` API break in gg is a finding, not something to fix here).

- [ ] **Step 5: Record findings and STOP for the user**

Append `## Spike findings (2026-09-24)` to the spec: per-check PASS/FAIL, the exact `x/vt` pseudo-version, whether `SafeEmulator` suffices, the dependency-bump result. Commit the spec change only. **If any of checks 1–4 fails on Linux, stop: Plan 1 is re-planned onto `hinshun/vt10x` before Task 1.** Report to the user and wait for the go-ahead.

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && git add docs/superpowers/specs/2026-09-24-agent-sessions-design.md && git commit -m "docs(spec): agent sessions — spike findings" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43"
```

---

### Task 1: Dependencies + `agentsession` package skeleton + DAG guard

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/agentsession/doc.go`, `internal/agentsession/types.go`
- Modify: `internal/archtest/import_guard_test.go` (add a `TestLayeringDAG` case)

**Interfaces:**
- Produces:
  ```go
  type ID string
  type State int
  const ( Running State = iota; Exited )
  type Info struct {
      ID       ID
      Label    string    // menu label, e.g. "Claude" / "Claude (yolo)"
      AgentID  string    // exttool tool id ("claude"), "" for custom
      Repo     string    // repo NAME for grouping (caller-computed)
      Dir      string    // worktree path = cwd
      Started  time.Time
      State    State
      ExitCode int       // valid when State == Exited; -1 = killed/unknown
  }
  type StartSpec struct {
      Label, AgentID, Repo, Dir string
      Argv       []string  // argv[0] resolved by exec.LookPath inside Start
      Env        []string  // extra KEY=VALUE, appended after os.Environ()
      Cols, Rows int
  }
  const ScrollbackLines = 10000
  ```

- [ ] **Step 1: Write the failing DAG test** — add to the `cases` map in `TestLayeringDAG`:

```go
		// agentsession is a leaf: the PTY/emulator core may reach no gigagit layer.
		"agentsession": {"config", "model", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app", "exttool", "template", "i18n"},
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test ./internal/archtest/ -run TestLayeringDAG`
Expected: FAIL — `go list .../internal/agentsession: ... no Go files` (or "cannot find package").

- [ ] **Step 3: Add deps and the skeleton**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go get github.com/charmbracelet/x/xpty@v0.1.4 github.com/charmbracelet/x/vt@<pseudo-version recorded in the spike>
```

`internal/agentsession/doc.go`:

```go
// Package agentsession runs interactive programs (AI agents, shells) in
// pseudo-terminals with an in-memory terminal emulator, so a frontend can
// paint a live console and forward keystrokes. A Manager owns every session
// of the process; nothing here knows about git, repos or the TUI — callers
// pass the working directory and the repo NAME used for grouping.
//
// Each Session runs three goroutines: PTY → emulator (the child's output),
// emulator → PTY (query replies such as a cursor-position report, plus keys
// and pastes encoded by the emulator), and a waiter that records the exit.
// Neither pump ever waits on a consumer: Changed is a capacity-1 coalesced
// signal and taps drop when full.
package agentsession
```

`internal/agentsession/types.go`: the types from *Produces* above, verbatim, with `import "time"` and one-line doc comments per type.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go build ./... && go test ./internal/archtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go mod tidy && git add go.mod go.sum internal/agentsession internal/archtest/import_guard_test.go && git commit -m "feat(agentsession): package skeleton, xpty + x/vt deps, DAG leaf guard" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43"
```

(`go mod tidy` drops the deps again if nothing imports them yet — if so, add `import _ "github.com/charmbracelet/x/vt"` and `_ ".../xpty"` in `doc.go` temporarily; Task 2 replaces them with real imports.)

---

### Task 2: `Session` — start, pumps, exit code, `Changed`

**Files:**
- Create: `internal/agentsession/session.go`, `internal/agentsession/proc_unix.go` (`//go:build !windows`), `internal/agentsession/proc_windows.go` (`//go:build windows`)
- Test: `internal/agentsession/session_test.go`

**Interfaces:**
- Consumes: Task 1 types.
- Produces:
  ```go
  func start(id ID, spec StartSpec) (*Session, error) // package-private; Manager.Start wraps it
  func (s *Session) Info() Info
  func (s *Session) Changed() <-chan struct{}         // capacity 1, coalesced
  func (s *Session) Done() <-chan struct{}            // closed after exit is recorded
  // proc_*.go
  func prepareCmd(cmd *exec.Cmd)                      // unix: Setsid+Setctty; windows: no-op
  ```

- [ ] **Step 1: Write the failing tests**

```go
package agentsession

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func needSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("scripted children use sh; the Windows path is covered by TestWindowsConPTY")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
}

func startSh(t *testing.T, script string) *Session {
	t.Helper()
	s, err := start("t1", StartSpec{Label: "sh", Dir: t.TempDir(), Argv: []string{"sh", "-c", script}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	return s
}

func waitDone(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("session did not exit")
	}
}

func TestSessionExitCode(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, "printf hello; exit 3")
	waitDone(t, s)
	info := s.Info()
	if info.State != Exited || info.ExitCode != 3 {
		t.Fatalf("got state=%v code=%d, want Exited/3", info.State, info.ExitCode)
	}
}

func TestSessionEnvAndDir(t *testing.T) {
	t.Parallel()
	needSh(t)
	dir := t.TempDir()
	s, err := start("envid", StartSpec{Dir: dir, Argv: []string{"sh", "-c", `printf '%s|%s|%s' "$GG_SESSION_ID" "$TERM" "$(pwd)"; sleep 1`}, Cols: 80, Rows: 5})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	got := s.screenText()
	if !strings.Contains(got, "envid|xterm-256color|") || !strings.Contains(got, dir[len(dir)-10:]) {
		t.Fatalf("screen = %q", got)
	}
}

func TestChangedCoalesces(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, "i=0; while [ $i -lt 2000 ]; do echo line$i; i=$((i+1)); done; sleep 0.2")
	waitDone(t, s)
	n := 0
	for {
		select {
		case <-s.Changed():
			n++
			continue
		default:
		}
		break
	}
	if n != 1 {
		t.Fatalf("pending Changed signals = %d, want exactly 1 (coalesced)", n)
	}
}

func TestEmulatorRepliesReachChild(t *testing.T) {
	t.Parallel()
	needSh(t)
	// Ask for the cursor position (ESC[6n) and read the reply (ESC[row;colR)
	// in raw mode; a missing emulator→PTY pump leaves `dd` blocked until timeout.
	s := startSh(t, `stty raw -echo; printf '\033[6n'; r=$(dd bs=1 count=6 2>/dev/null | od -An -c | tr -d ' \n'); stty sane; printf 'GOT[%s]' "$r"; sleep 0.5`)
	waitDone(t, s)
	if got := s.screenText(); !strings.Contains(got, "GOT[033[1;1R") {
		t.Fatalf("cursor-position reply not delivered; screen = %q", got)
	}
}
```

Also add, to `session.go`'s test surface, `func (s *Session) screenText() string` (plain text of the visible grid, used only by tests and by `Screen` in Task 3).

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test ./internal/agentsession/`
Expected: FAIL — `undefined: start`.

- [ ] **Step 3: Implement**

`proc_unix.go`:

```go
//go:build !windows

package agentsession

import (
	"os/exec"
	"syscall"
)

// prepareCmd makes the child a session leader with the PTY as its
// controlling terminal, so job control works and a kill can target the
// whole process group (-pid).
func prepareCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
}
```

`proc_windows.go`:

```go
//go:build windows

package agentsession

import "os/exec"

// prepareCmd is a no-op on Windows: ConPTY attaches the console itself, and
// the job object (kill_windows.go) groups the process tree.
func prepareCmd(*exec.Cmd) {}
```

`session.go`:

```go
package agentsession

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
)

// Session is one running (or exited) program in a PTY.
type Session struct {
	mu   sync.Mutex
	info Info
	pty  xpty.Pty
	emu  *vt.SafeEmulator
	cmd  *exec.Cmd

	changed      chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
	cursorHidden atomic.Bool // DECTCEM state, fed by the emulator callback
}

func start(id ID, spec StartSpec) (*Session, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("agentsession: empty argv")
	}
	bin, err := exec.LookPath(spec.Argv[0])
	if err != nil {
		return nil, err
	}
	cols, rows := max(spec.Cols, 20), max(spec.Rows, 5)
	p, err := xpty.NewPty(cols, rows)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(append(os.Environ(), spec.Env...), "GG_SESSION_ID="+string(id), "TERM=xterm-256color")
	prepareCmd(cmd)
	if err := p.Start(cmd); err != nil {
		_ = p.Close()
		return nil, err
	}
	if up, ok := p.(*xpty.UnixPty); ok {
		_ = up.Slave().Close() // the child holds its own copy; ours would block EOF/EIO forever
	}
	emu := vt.NewSafeEmulator(cols, rows)
	emu.SetScrollbackSize(ScrollbackLines)
	s := &Session{
		info: Info{ID: id, Label: spec.Label, AgentID: spec.AgentID, Repo: spec.Repo, Dir: spec.Dir,
			Started: time.Now(), State: Running},
		pty: p, emu: emu, cmd: cmd,
		changed: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	// Callbacks run inside emu.Write under the emulator's lock: store only.
	emu.SetCallbacks(vt.Callbacks{CursorVisibility: func(v bool) { s.cursorHidden.Store(!v) }})
	attachJob(s) // kill_*.go (Task 4); no-op on unix
	go s.pumpOut()
	go s.pumpIn()
	go s.wait()
	return s, nil
}

// pumpOut copies the child's output into the emulator. Linux reports EIO on
// the master once every slave fd is closed: that is end-of-stream, not a
// failure.
func (s *Session) pumpOut() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			_, _ = s.emu.Write(buf[:n])
			s.signal()
		}
		if err != nil {
			return
		}
	}
}

// pumpIn drains what the emulator emits (query replies, encoded keys,
// bracketed pastes) into a queue that a separate writer feeds to the child.
// The emulator's output is a synchronous io.Pipe written UNDER the
// emulator's lock (inside Write for replies, inside SendKey for keys), so
// this reader must never stall: a child that stops reading its stdin would
// otherwise block pumpOut (Write holds the lock) and the caller of SendKey.
// After a PTY write error the writer keeps draining and discarding until
// the emulator is closed. Ends at emulator Close (Read → EOF).
func (s *Session) pumpIn() {
	q := make(chan []byte, 1024)
	go func() {
		dead := false
		for p := range q {
			if !dead {
				if _, err := s.pty.Write(p); err != nil {
					dead = true
				}
			}
		}
	}()
	defer close(q)
	buf := make([]byte, 4096)
	for {
		n, err := s.emu.Read(buf)
		if n > 0 {
			q <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) wait() {
	err := xpty.WaitProcess(context.Background(), s.cmd)
	code := 0
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode() // -1 when killed by a signal
	default:
		code = -1
	}
	s.mu.Lock()
	s.info.State, s.info.ExitCode = Exited, code
	s.mu.Unlock()
	s.closeIO()
	s.signal()
	close(s.done)
}

func (s *Session) closeIO() {
	s.closeOnce.Do(func() {
		_ = s.emu.Close()
		_ = s.pty.Close()
	})
}

// signal marks the screen dirty without ever blocking the pump.
func (s *Session) signal() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *Session) Info() Info              { s.mu.Lock(); defer s.mu.Unlock(); return s.info }
func (s *Session) Changed() <-chan struct{} { return s.changed }
func (s *Session) Done() <-chan struct{}    { return s.done }

func (s *Session) screenText() string {
	var b strings.Builder
	w, h := s.emu.Width(), s.emu.Height()
	for y := range h {
		for x := range w {
			if c := s.emu.CellAt(x, y); c != nil && c.Content != "" {
				b.WriteString(c.Content)
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
```

Add a temporary `func (s *Session) kill() { if s.cmd.Process != nil { _ = s.cmd.Process.Kill() } }` and `func attachJob(*Session) {}` at the bottom of `session.go` so the tests compile; Task 4 replaces both with the real per-OS files. (If the spike found `SafeEmulator` lacking `CellAt`/`SendKey`/`Render`, use `*vt.Emulator` plus `s.mu` around every emulator call — the spike note says which.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test -race ./internal/agentsession/`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && gofmt -l internal/agentsession; git add internal/agentsession && git commit -m "feat(agentsession): PTY session with emulator pumps, coalesced Changed, exit code" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43"
```

---

### Task 3: Input, resize, screen snapshot, scrollback

**Files:**
- Create: `internal/agentsession/io.go`
- Test: `internal/agentsession/io_test.go`

**Interfaces:**
- Consumes: `Session` (Task 2).
- Produces:
  ```go
  type Key = uv.KeyEvent                          // re-exported so callers need not import ultraviolet
  type Screen struct {
      Lines    []string  // one ANSI-styled line per row (Emulator.Render split on "\n", padded to Rows)
      Cols, Rows int
      CursorX, CursorY int
      CursorVisible bool
      AltScreen bool
  }
  func (s *Session) SendKey(k Key)                // encoded by the emulator (honours cursor-key mode)
  func (s *Session) SendText(text string)         // literal runes
  func (s *Session) Paste(text string)            // bracketed when the child enabled it
  func (s *Session) Resize(cols, rows int) error  // emulator + PTY; no-op when unchanged
  func (s *Session) Screen() Screen
  func (s *Session) ScrollbackLen() int
  ```
  All input methods are no-ops once `State == Exited`.

- [ ] **Step 1: Write the failing tests**

```go
package agentsession

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSendTextAndEnterEcho(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `read line; printf 'ECHO[%s]' "$line"; sleep 1`)
	s.SendText("hi there")
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	eventually(t, "echo", func() bool { return strings.Contains(s.screenText(), "ECHO[hi there]") })
}

func TestResizeReachesChild(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `read _; stty size; sleep 1`)
	if err := s.Resize(61, 17); err != nil {
		t.Fatal(err)
	}
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	eventually(t, "stty size", func() bool { return strings.Contains(s.screenText(), "17 61") })
	if sc := s.Screen(); sc.Cols != 61 || sc.Rows != 17 || len(sc.Lines) != 17 {
		t.Fatalf("screen dims = %dx%d lines=%d", sc.Cols, sc.Rows, len(sc.Lines))
	}
}

func TestScreenCursorAndAltScreen(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `printf '\033[?1049h\033[3;5HX'; sleep 2`)
	eventually(t, "alt screen", func() bool { return s.Screen().AltScreen })
	sc := s.Screen()
	if !strings.Contains(sc.Lines[2], "X") || sc.CursorY != 2 || sc.CursorX != 5 {
		t.Fatalf("cursor=(%d,%d) line2=%q", sc.CursorX, sc.CursorY, sc.Lines[2])
	}
}

func TestScrollbackCapped(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `i=0; while [ $i -lt 12000 ]; do echo $i; i=$((i+1)); done`)
	waitDone(t, s)
	if n := s.ScrollbackLen(); n > ScrollbackLines || n < ScrollbackLines-20 {
		t.Fatalf("scrollback = %d, want ≈%d (capped)", n, ScrollbackLines)
	}
}

func TestPasteToNonReaderDoesNotBlock(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `sleep 5`) // never reads stdin
	done := make(chan struct{})
	go func() {
		s.Paste(strings.Repeat("x", 256*1024))
		s.SendText("more")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Paste blocked on a child that does not read stdin")
	}
	_ = s.Screen() // the emulator lock must still be free
}

func TestInputAfterExitIsNoop(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `exit 0`)
	waitDone(t, s)
	s.SendText("x")                                    // must not panic / block
	s.Paste("y")
	if err := s.Resize(50, 10); err != nil {
		t.Fatalf("resize after exit = %v, want nil no-op", err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test ./internal/agentsession/ -run 'Send|Resize|Screen|Scrollback|AfterExit'`
Expected: FAIL — `s.SendText undefined`.

- [ ] **Step 3: Implement `io.go`**

```go
package agentsession

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Key is one keystroke for SendKey, encoded by the emulator so the child's
// terminal modes (application cursor keys, kitty flags) are honoured.
type Key = uv.KeyEvent

// Screen is a copied snapshot of the visible grid.
type Screen struct {
	Lines            []string
	Cols, Rows       int
	CursorX, CursorY int
	CursorVisible    bool
	AltScreen        bool
}

func (s *Session) running() bool { return s.Info().State == Running }

func (s *Session) SendKey(k Key) {
	if s.running() {
		s.emu.SendKey(k)
	}
}

func (s *Session) SendText(text string) {
	if s.running() {
		s.emu.SendText(text)
	}
}

func (s *Session) Paste(text string) {
	if s.running() {
		s.emu.Paste(text)
	}
}

func (s *Session) Resize(cols, rows int) error {
	if !s.running() || (cols == s.emu.Width() && rows == s.emu.Height()) {
		return nil
	}
	cols, rows = max(cols, 20), max(rows, 5)
	s.emu.Resize(cols, rows)
	err := s.pty.Resize(cols, rows)
	s.signal()
	return err
}

func (s *Session) Screen() Screen {
	w, h := s.emu.Width(), s.emu.Height()
	lines := strings.Split(s.emu.Render(), "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	pos := s.emu.CursorPosition()
	return Screen{
		Lines: lines[:h], Cols: w, Rows: h,
		CursorX: pos.X, CursorY: pos.Y,
		CursorVisible: !s.cursorHidden.Load(),
		AltScreen:     s.emu.IsAltScreen(),
	}
}

func (s *Session) ScrollbackLen() int { return s.emu.ScrollbackLen() }
```

(`SafeEmulator` has no cursor-visibility getter; the `CursorVisibility` callback installed in `start` feeds `cursorHidden`.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test -race ./internal/agentsession/`
Expected: PASS (10 tests).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && git add internal/agentsession && git commit -m "feat(agentsession): key/text/paste input, resize, screen snapshot, capped scrollback" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43"
```

---

### Task 4: Kill (process group / job object), `Manager`, raw tap

**Files:**
- Create: `internal/agentsession/kill_unix.go`, `internal/agentsession/kill_windows.go`, `internal/agentsession/manager.go`, `internal/agentsession/tap.go`
- Modify: `internal/agentsession/session.go` (delete the temporary `kill`/`attachJob`; `pumpOut` feeds taps)
- Test: `internal/agentsession/manager_test.go`, `internal/agentsession/windows_test.go` (`//go:build windows`)

**Interfaces:**
- Consumes: Tasks 2–3.
- Produces:
  ```go
  type Manager struct{ /* unexported */ }
  func NewManager() *Manager
  func (m *Manager) Start(spec StartSpec) (*Session, error)  // assigns ID "s1", "s2", …
  func (m *Manager) Get(id ID) (*Session, bool)
  func (m *Manager) List() []Info                             // sorted by Started, then ID
  func (m *Manager) LiveCount() int
  func (m *Manager) Kill(id ID) error                         // ErrNoSession; no-op on exited
  func (m *Manager) Remove(id ID) error                       // ErrRunning if still running
  func (m *Manager) KillAll(ctx context.Context)              // returns when all exited or ctx done
  func (m *Manager) Changed() <-chan struct{}                 // list-level: start/exit/remove, coalesced
  var ErrNoSession, ErrRunning error
  func (s *Session) Tap() (<-chan []byte, func())             // raw child output, 64-deep, drops when full
  ```

- [ ] **Step 1: Write the failing tests**

```go
package agentsession

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestManagerLifecycle(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	a, err := m.Start(StartSpec{Label: "a", Dir: t.TempDir(), Argv: []string{"sh", "-c", "sleep 30"}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Start(StartSpec{Label: "b", Dir: t.TempDir(), Argv: []string{"sh", "-c", "exit 4"}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, b)
	if got := m.LiveCount(); got != 1 {
		t.Fatalf("LiveCount = %d, want 1", got)
	}
	l := m.List()
	if len(l) != 2 || l[0].ID != a.Info().ID || l[1].ExitCode != 4 {
		t.Fatalf("List = %+v", l)
	}
	if err := m.Remove(a.Info().ID); !errors.Is(err, ErrRunning) {
		t.Fatalf("Remove(running) = %v, want ErrRunning", err)
	}
	if err := m.Remove(b.Info().ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m.KillAll(ctx)
	if m.LiveCount() != 0 {
		t.Fatal("KillAll left a live session")
	}
}

func TestKillTakesProcessGroup(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	// The shell backgrounds a grandchild that ignores nothing and prints its pid.
	s, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", `sleep 60 & echo "GC=$!"; wait`}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	var gc int
	eventually(t, "grandchild pid", func() bool {
		txt := s.screenText()
		i := strings.Index(txt, "GC=")
		if i < 0 {
			return false
		}
		f := strings.Fields(txt[i+3:])
		gc, _ = strconv.Atoi(f[0])
		return gc > 0
	})
	if err := m.Kill(s.Info().ID); err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	eventually(t, "grandchild gone", func() bool {
		p, _ := os.FindProcess(gc)
		return p.Signal(syscall.Signal(0)) != nil
	})
}

func TestLifecycleMisuse(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	if err := m.Kill("nope"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Kill(unknown) = %v", err)
	}
	s, _ := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", "exit 0"}, Cols: 40, Rows: 10})
	waitDone(t, s)
	if err := m.Kill(s.Info().ID); err != nil { // exited: no-op
		t.Fatalf("Kill(exited) = %v", err)
	}
	if err := m.Kill(s.Info().ID); err != nil { // twice: still no-op
		t.Fatalf("second Kill = %v", err)
	}
	s.SendText("late") // must not panic
}

func TestTapDropsWhenFull(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	s, _ := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", `sleep 0.3; i=0; while [ $i -lt 5000 ]; do echo $i; i=$((i+1)); done`}, Cols: 40, Rows: 10})
	ch, cancel := s.Tap() // never read: the child must still run to completion
	defer cancel()
	waitDone(t, s)
	if len(ch) == 0 {
		t.Fatal("tap received nothing")
	}
}

func TestManagerChangedOnStartAndExit(t *testing.T) {
	t.Parallel()
	needSh(t)
	m := NewManager()
	s, _ := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"sh", "-c", "exit 0"}, Cols: 40, Rows: 10})
	waitDone(t, s)
	select {
	case <-m.Changed():
	case <-time.After(2 * time.Second):
		t.Fatal("no manager-level change signal")
	}
}
```

`windows_test.go`:

```go
//go:build windows

package agentsession

import (
	"strings"
	"testing"
)

func TestWindowsConPTY(t *testing.T) {
	m := NewManager()
	s, err := m.Start(StartSpec{Dir: t.TempDir(), Argv: []string{"cmd", "/c", "echo WINOK & exit /b 5"}, Cols: 60, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, s)
	if !strings.Contains(s.screenText(), "WINOK") || s.Info().ExitCode != 5 {
		t.Fatalf("screen=%q code=%d", s.screenText(), s.Info().ExitCode)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test ./internal/agentsession/ -run 'Manager|Kill|Misuse|Tap'`
Expected: FAIL — `undefined: NewManager`.

- [ ] **Step 3: Implement**

`kill_unix.go`:

```go
//go:build !windows

package agentsession

import (
	"syscall"
	"time"
)

func attachJob(*Session) {}

// kill sends SIGTERM to the whole process group (the child is a session
// leader, so -pid is its group), then SIGKILL if it outlives the grace.
func (s *Session) kill() {
	if s.cmd.Process == nil || !s.running() {
		return
	}
	pid := s.cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()
}
```

`kill_windows.go`:

```go
//go:build windows

package agentsession

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// attachJob puts the child in a kill-on-close job object, so terminating
// the job (or gg dying and the handle closing) takes the whole tree.
func attachJob(s *Session) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	_, _ = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(s.cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer windows.CloseHandle(h)
	if windows.AssignProcessToJobObject(job, h) != nil {
		_ = windows.CloseHandle(job)
		return
	}
	s.job = uintptr(job)
}

func (s *Session) kill() {
	if !s.running() {
		return
	}
	if s.job != 0 {
		_ = windows.TerminateJobObject(windows.Handle(s.job), 1)
	} else if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.closeIO()
}
```

Add `job uintptr // windows job object handle; 0 elsewhere` to `Session`.

`tap.go`:

```go
package agentsession

// Tap returns a channel receiving copies of the child's raw output (for a
// future web attach) and a cancel func. A slow reader loses chunks — the
// pump never waits for it.
func (s *Session) Tap() (<-chan []byte, func()) {
	ch := make(chan []byte, 64)
	s.mu.Lock()
	if s.taps == nil {
		s.taps = map[chan []byte]struct{}{}
	}
	s.taps[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.taps, ch)
		s.mu.Unlock()
	}
}

func (s *Session) feedTaps(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.taps {
		select {
		case ch <- append([]byte(nil), p...):
		default:
		}
	}
}
```

Add `taps map[chan []byte]struct{}` to `Session`; in `pumpOut` call `s.feedTaps(buf[:n])` after `s.emu.Write`.

`manager.go`:

```go
package agentsession

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync"
)

var (
	ErrNoSession = errors.New("agentsession: no such session")
	ErrRunning   = errors.New("agentsession: session is still running")
)

// Manager owns every session of the process.
type Manager struct {
	mu       sync.Mutex
	next     int
	sessions map[ID]*Session
	changed  chan struct{}
}

func NewManager() *Manager {
	return &Manager{sessions: map[ID]*Session{}, changed: make(chan struct{}, 1)}
}

func (m *Manager) signal() {
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

func (m *Manager) Changed() <-chan struct{} { return m.changed }

func (m *Manager) Start(spec StartSpec) (*Session, error) {
	m.mu.Lock()
	m.next++
	id := ID("s" + strconv.Itoa(m.next))
	m.mu.Unlock()
	s, err := start(id, spec)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	m.signal()
	go func() { <-s.Done(); m.signal() }()
	return s, nil
}

func (m *Manager) Get(id ID) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *Manager) List() []Info {
	m.mu.Lock()
	out := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info())
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Started.Equal(out[j].Started) {
			return out[i].Started.Before(out[j].Started)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *Manager) LiveCount() int {
	n := 0
	for _, i := range m.List() {
		if i.State == Running {
			n++
		}
	}
	return n
}

func (m *Manager) Kill(id ID) error {
	s, ok := m.Get(id)
	if !ok {
		return ErrNoSession
	}
	s.kill()
	return nil
}

func (m *Manager) Remove(id ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrNoSession
	}
	if s.Info().State == Running {
		return ErrRunning
	}
	delete(m.sessions, id)
	m.signal()
	return nil
}

func (m *Manager) KillAll(ctx context.Context) {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	for _, s := range all {
		s.kill()
	}
	for _, s := range all {
		select {
		case <-s.Done():
		case <-ctx.Done():
			return
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test -race ./internal/agentsession/ && GOOS=windows go vet ./internal/agentsession/`
Expected: PASS (15 tests); the Windows vet compiles `kill_windows.go`/`windows_test.go` cleanly.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && git add internal/agentsession && git commit -m "feat(agentsession): Manager, process-group/job-object kill, raw output tap" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43"
```

---

### Task 5: exttool `session` category + config acceptance

**Files:**
- Modify: `internal/exttool/exttool.go` (constants, `Builtins()` rows)
- Modify: `internal/config/tools.go` (`ValidateToolCommand`)
- Modify: every consumer that lists tool commands without filtering by category (found in Step 3)
- Test: `internal/exttool/exttool_test.go`, `internal/config/tools_test.go`, `internal/tui/settings_tools_test.go`

**Interfaces:**
- Produces: `exttool.CatSession Category = "session"`, `exttool.ModeSession Mode = "session"`; config accepts `category = "session"` only with `mode = "session"` and vice versa.

- [ ] **Step 1: Verify the yolo flags against the real CLIs**

```bash
for b in claude codex junie agy kimi; do command -v $b >/dev/null && { echo "== $b"; $b --help 2>&1 | grep -iE "dangerously|brave|yolo|bypass" ; }; done
```

Record which flags exist. A flag not listed by `--help` (or a binary not installed here) keeps its row only if the existing conflict templates already use that exact flag (they do for claude/codex/agy/junie `--brave`); otherwise drop the yolo row.

- [ ] **Step 2: Write the failing tests**

`internal/exttool/exttool_test.go`:

```go
func TestSessionBuiltins(t *testing.T) {
	want := map[string][]string{ // tool id → session command names
		"claude":      {"Claude", "Claude (yolo)"},
		"codex":       {"Codex", "Codex (yolo)"},
		"junie":       {"Junie", "Junie (yolo)"},
		"antigravity": {"Antigravity", "Antigravity (yolo)"},
		"kimi":        {"Kimi"},
	}
	for _, tl := range Builtins() {
		var got []string
		for _, c := range tl.Commands {
			if c.Category != CatSession {
				continue
			}
			if c.Mode != ModeSession {
				t.Errorf("%s/%s: mode %q, want session", tl.ID, c.Name, c.Mode)
			}
			if strings.HasSuffix(c.Name, "(yolo)") != c.OptIn {
				t.Errorf("%s/%s: OptIn=%v must match the (yolo) suffix", tl.ID, c.Name, c.OptIn)
			}
			if !strings.HasPrefix(c.Command, "<bin>") {
				t.Errorf("%s/%s: command %q must start with <bin>", tl.ID, c.Name, c.Command)
			}
			got = append(got, c.Name)
		}
		if w, ok := want[tl.ID]; ok && !slices.Equal(got, w) {
			t.Errorf("%s session commands = %v, want %v", tl.ID, got, w)
		}
	}
}
```

`internal/config/tools_test.go` — extend `TestValidateToolCommand`'s table:

```go
		{"session ok", ToolCommand{Category: "session", Name: "Claude", Mode: "session", Command: "claude"}, ""},
		{"session needs session mode", ToolCommand{Category: "session", Name: "C", Mode: "terminal", Command: "claude"}, "mode"},
		{"session mode only for session", ToolCommand{Category: "review", Name: "R", Mode: "session", Command: "x"}, "mode"},
```

(match the existing table's field names; the third column is a substring the error must contain, "" = valid.)

`internal/tui/settings_tools_test.go` — `defaultToolChecked` leaves `Claude (yolo)` session row unchecked and `Claude` session row checked:

```go
func TestToolsWizardSessionRowsDefaults(t *testing.T) {
	rows := []toolWizardRow{
		{tmpl: exttool.CommandTemplate{Category: exttool.CatSession, Name: "Claude", Mode: exttool.ModeSession}},
		{tmpl: exttool.CommandTemplate{Category: exttool.CatSession, Name: "Claude (yolo)", Mode: exttool.ModeSession, OptIn: true}},
	}
	got := defaultToolChecked(rows)
	if !got[0] || got[1] {
		t.Fatalf("checked = %v, want [true false]", got)
	}
}
```

- [ ] **Step 3: Run them to verify they fail, and find category-blind consumers**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test ./internal/exttool/ ./internal/config/ ./internal/tui/ -run 'Session|ValidateToolCommand'`
Expected: FAIL — `undefined: CatSession`.

Then: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && grep -rn "Tools.Command\b\|range .*Tools.Command\|\.Mode == \"terminal\"\|ModeTerminal\|ModeCapture" internal --include='*.go' | grep -v _test` — for each loop that picks commands for a lane, confirm it filters by `Category`; any that switches on `Mode` with a default branch must treat `"session"` as not-its-business (skip). List them in the commit message.

- [ ] **Step 4: Implement**

In `exttool.go`, add to the `Category` block:

```go
	// CatSession is an interactive agent session run in gg's own embedded
	// console (internal/agentsession) under a chosen worktree — neither a
	// terminal handover nor a headless capture, so it has its own mode.
	CatSession Category = "session"
```

and to the `Mode` block:

```go
	ModeSession Mode = "session"
```

In `Builtins()`, append to each tool's `Commands` (keeping the verified flags from Step 1):

```go
				{Category: CatSession, Name: "Claude", Mode: ModeSession, Command: "<bin>"},
				{Category: CatSession, Name: "Claude (yolo)", Mode: ModeSession, OptIn: true, Command: "<bin> --dangerously-skip-permissions"},
```
```go
				{Category: CatSession, Name: "Junie", Mode: ModeSession, Command: "<bin>"},
				{Category: CatSession, Name: "Junie (yolo)", Mode: ModeSession, OptIn: true, Command: "<bin> --brave"},
```
```go
				{Category: CatSession, Name: "Codex", Mode: ModeSession, Command: "<bin>"},
				{Category: CatSession, Name: "Codex (yolo)", Mode: ModeSession, OptIn: true, Command: "<bin> --dangerously-bypass-approvals-and-sandbox"},
```
```go
				{Category: CatSession, Name: "Antigravity", Mode: ModeSession, Command: "<bin>"},
				{Category: CatSession, Name: "Antigravity (yolo)", Mode: ModeSession, OptIn: true, Command: "<bin> --dangerously-skip-permissions"},
```
```go
				{Category: CatSession, Name: "Kimi", Mode: ModeSession, Command: "<bin>"},
```

Update the package doc's category list sentence to include "interactive agent sessions".

In `config/tools.go` `ValidateToolCommand`: add `"session"` to the category switch (and to the error's `want` list), add `"session"` to the mode switch (and its `want` list), then after the mode switch:

```go
	if (tc.Category == "session") != (tc.Mode == "session") {
		return fmt.Errorf("tools: %s: category \"session\" requires mode = \"session\" (and that mode is only for sessions)", tc.Name)
	}
```

Update the `ToolCommand` field comments (`// conflict | commit_message | review | conflict_complete | session`, `// terminal | capture | session`). Apply the Step 3 consumer fixes.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test ./internal/exttool/ ./internal/config/ ./internal/tui/ ./internal/web/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && git add -A internal && git commit -m "feat(exttool): session category + mode with per-agent built-ins (yolo opt-in)" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43"
```

---

### Task 6: domain — process-global manager, session commands, start, first-run auto-configure

**Files:**
- Create: `internal/domain/sessions.go`
- Test: `internal/domain/sessions_test.go`
- Modify: `internal/archtest/import_guard_test.go` (frontends may not import `agentsession`)

**Interfaces:**
- Consumes: `agentsession.Manager` API (Task 4), `exttool.CatSession/ModeSession` (Task 5), `config.ToolCommand`, `config.AppendToolCommands`, `config.ToolVisibleIn`, `config.ValidateToolCommand`, `exttool.Detect`, `exttool.GenerateCommand`, `template.ResolveCommand`, `template.FlattenForCmd`, `(*Service).RepoName`.
- Produces (the exact surface Plan 2 builds on):
  ```go
  type SessionID     = agentsession.ID
  type SessionInfo   = agentsession.Info
  type SessionState  = agentsession.State
  const SessionRunning, SessionExited = agentsession.Running, agentsession.Exited
  type AgentSession  = agentsession.Session
  type SessionScreen = agentsession.Screen
  type SessionKey    = agentsession.Key

  func Sessions() *agentsession.Manager                  // process-global, lazily created
  func UseSessionManager(m *agentsession.Manager) func() // test seam; returns restore

  // SessionCommands: effective-config `session` commands visible in frontend.
  func SessionCommands(cfg config.Config, frontend string) []config.ToolCommand

  // EnsureSessionCommands: if cfg has no session command, detect installed
  // agents and append their SAFE (non-OptIn) session blocks to globalPath.
  // Returns the names added (nil when nothing was needed or nothing found).
  func EnsureSessionCommands(cfg config.Config, globalPath string, detect func() []exttool.Detection) ([]string, error)

  // StartSession resolves tc for worktreeDir and starts it.
  func (s *Service) StartSession(ctx context.Context, tc config.ToolCommand, worktreeDir string, cols, rows int) (*AgentSession, error)
  ```

- [ ] **Step 1: Write the failing tests**

```go
package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

func TestSessionsIsProcessGlobal(t *testing.T) {
	if Sessions() != Sessions() {
		t.Fatal("Sessions() must return one manager for the process")
	}
	m := agentsession.NewManager()
	restore := UseSessionManager(m)
	if Sessions() != m {
		t.Fatal("UseSessionManager did not install the manager")
	}
	restore()
	if Sessions() == m {
		t.Fatal("restore did not reinstall the previous manager")
	}
}

func TestSessionCommandsFilters(t *testing.T) {
	t.Parallel()
	cfg := config.Config{}
	cfg.Tools.Command = []config.ToolCommand{
		{Category: "review", Name: "R", Mode: "capture", Command: "x"},
		{Category: "session", Name: "Claude", Mode: "session", Command: "claude"},
		{Category: "session", Name: "WebOnly", Mode: "session", Command: "x", Frontends: []string{"web"}},
		{Category: "session", Name: "", Mode: "session", Command: "broken"}, // invalid → inert
	}
	got := SessionCommands(cfg, "tui")
	if len(got) != 1 || got[0].Name != "Claude" {
		t.Fatalf("SessionCommands = %+v", got)
	}
}

func fakeDetect() []exttool.Detection {
	var out []exttool.Detection
	for _, tl := range exttool.Builtins() {
		if tl.ID == "claude" || tl.ID == "codex" {
			out = append(out, exttool.Detection{Tool: tl, Bin: tl.ID})
		}
	}
	return out
}

func TestEnsureSessionCommandsWritesSafeOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	added, err := EnsureSessionCommands(config.Config{}, path, fakeDetect)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(added, ",") != "Claude,Codex" {
		t.Fatalf("added = %v", added)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "yolo") || !strings.Contains(string(body), `category = "session"`) {
		t.Fatalf("config body:\n%s", body)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := EnsureSessionCommands(cfg, path, fakeDetect); again != nil {
		t.Fatalf("second run added %v, want nil (already configured)", again)
	}
}

func TestEnsureSessionCommandsNothingDetected(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	added, err := EnsureSessionCommands(config.Config{}, path, func() []exttool.Detection { return nil })
	if err != nil || added != nil {
		t.Fatalf("added=%v err=%v", added, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("nothing detected must not create the config file")
	}
}

func TestStartSessionRunsInWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := UseSessionManager(agentsession.NewManager())
	defer restore()
	dir := newTestRepo(t) // existing domain test helper: a real repo in t.TempDir()
	svc := Open(dir)
	tc := config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: `printf 'IN[%s]' "$(pwd)"; exit 7`}
	s, err := svc.StartSession(context.Background(), tc, dir, 80, 10)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("no exit")
	}
	info := s.Info()
	if info.ExitCode != 7 || info.Dir != dir || info.Label != "Shell" {
		t.Fatalf("info = %+v", info)
	}
	if len(Sessions().List()) != 1 {
		t.Fatal("session not registered with the process-global manager")
	}
}
```

(If the domain test helper is named differently, use whichever `domain` test helper builds a real repo — grep `func newTestRepo\|func newRepo` in `internal/domain/*_test.go`.)

Archtest — add to `TestFrontendsDoNotImportGit`'s `forbidden` map:

```go
		"github.com/homeend/gigagit/internal/agentsession": "frontends must reach agent sessions through internal/domain",
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test ./internal/domain/ -run 'Session'`
Expected: FAIL — `undefined: Sessions`.

- [ ] **Step 3: Implement `internal/domain/sessions.go`**

```go
package domain

import (
	"context"
	"os"
	"runtime"
	"sync"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

// Aliases so frontends use sessions without importing agentsession
// (archtest forbids that edge).
type (
	SessionID     = agentsession.ID
	SessionInfo   = agentsession.Info
	SessionState  = agentsession.State
	AgentSession  = agentsession.Session
	SessionScreen = agentsession.Screen
	SessionKey    = agentsession.Key
)

const (
	SessionRunning = agentsession.Running
	SessionExited  = agentsession.Exited
)

var (
	sessionsMu  sync.Mutex
	sessionsMgr *agentsession.Manager
)

// Sessions is the process-global session manager. It lives outside every
// Service because the TUI reopens its Service on each worktree/repo switch
// (reRoot) while sessions must keep running — the repogate registry
// precedent.
func Sessions() *agentsession.Manager {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if sessionsMgr == nil {
		sessionsMgr = agentsession.NewManager()
	}
	return sessionsMgr
}

// UseSessionManager installs m as the process-global manager (tests) and
// returns a func restoring the previous one.
func UseSessionManager(m *agentsession.Manager) func() {
	sessionsMu.Lock()
	prev := sessionsMgr
	sessionsMgr = m
	sessionsMu.Unlock()
	return func() {
		sessionsMu.Lock()
		sessionsMgr = prev
		sessionsMu.Unlock()
	}
}

// SessionCommands returns the effective config's valid `session` commands
// offered in frontend ("tui"|"web"|"cli"), in config order.
func SessionCommands(cfg config.Config, frontend string) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(exttool.CatSession) || config.ValidateToolCommand(tc) != nil || !config.ToolVisibleIn(tc, frontend) {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// EnsureSessionCommands is the first-run auto-configure: when cfg has no
// session command, detect installed agents and append their SAFE session
// templates (never an OptIn/yolo one) to the global config at globalPath.
// Returns the names added; nil when already configured or nothing found.
func EnsureSessionCommands(cfg config.Config, globalPath string, detect func() []exttool.Detection) ([]string, error) {
	if len(SessionCommands(cfg, "tui")) > 0 {
		return nil, nil
	}
	var blocks []config.ToolCommand
	var names []string
	for _, det := range detect() {
		for _, ct := range det.Tool.Commands {
			if ct.Category != exttool.CatSession || ct.OptIn {
				continue
			}
			blocks = append(blocks, config.ToolCommand{
				Category: string(ct.Category), Name: ct.Name, Mode: string(ct.Mode),
				Frontends: ct.Frontends, Command: exttool.GenerateCommand(ct, det.Bin),
			})
			names = append(names, ct.Name)
		}
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	if err := config.AppendToolCommands(globalPath, blocks); err != nil {
		return nil, err
	}
	return names, nil
}

// StartSession resolves tc (runtime tokens such as <repo>) and runs it in
// worktreeDir through the user's shell, registered with Sessions(). The
// session is grouped under the repository NAME (RepoName), falling back to
// the directory's base name.
func (s *Service) StartSession(ctx context.Context, tc config.ToolCommand, worktreeDir string, cols, rows int) (*AgentSession, error) {
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: worktreeDir})
	if err != nil {
		return nil, err
	}
	repo, err := s.RepoName(ctx)
	if err != nil || repo == "" {
		repo = filepathBase(worktreeDir)
	}
	return Sessions().Start(agentsession.StartSpec{
		Label: tc.Name, AgentID: agentIDFor(tc), Repo: repo, Dir: worktreeDir,
		Argv: shellArgv(resolved), Cols: cols, Rows: rows,
	})
}

// shellArgv runs the resolved command line the way external tools run
// (tool_run.go's toolExecCmd): $SHELL -c on POSIX with `exec` so the
// agent's own exit code is the session's; %COMSPEC% /C on Windows, with the
// multi-line template flattened for cmd.exe.
func shellArgv(cmdline string) []string {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd"
		}
		return []string{comspec, "/C", template.FlattenForCmd(cmdline)}
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return []string{sh, "-c", "exec " + cmdline}
}

// agentIDFor maps a command to its catalog tool id by the command's first
// word, "" for a custom command.
func agentIDFor(tc config.ToolCommand) string {
	first := firstWord(tc.Command)
	for _, tl := range exttool.Builtins() {
		for _, b := range tl.Bins {
			if filepathBase(first) == b || filepathBase(first) == b+".exe" {
				return tl.ID
			}
		}
	}
	return ""
}
```

Add two small helpers at the bottom (`filepathBase` = `filepath.Base` wrapper is unnecessary — import `path/filepath` and call `filepath.Base` directly; `firstWord(s string) string` = `strings.Fields(strings.Trim(s, `"`))[0]` guarded for empty). Note: `exec` is dropped by the shell for a compound command (`a; b`) — that is fine, the last command's status is the shell's.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && go test -race ./internal/domain/ -run 'Session' && go test ./internal/archtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && git add internal/domain/sessions.go internal/domain/sessions_test.go internal/archtest/import_guard_test.go && git commit -m "feat(domain): process-global agent sessions, session commands, first-run auto-configure" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43"
```

---

### Task 7: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md`, `CLAUDE.md` (package map row), `docs/CLAUDE-details.md`

- [ ] **Step 1: Docs**

`CLAUDE.md` package map — add after `exttool`:

```
| `agentsession` | Interactive programs (AI agents) in PTYs (`x/xpty`: creack/pty, ConPTY) with an in-memory `x/vt` emulator: `Manager` (Start/List/Kill/Remove/KillAll), `Session` (SendKey/Paste/Resize/Screen, coalesced `Changed`, raw `Tap`), process-group / job-object kill, 10k-line scrollback. DAG leaf; the one process-global manager lives in `domain` (survives `reRoot`). |
```

`docs/CLAUDE-details.md` — a new `## agentsession` section: the three goroutines, why `pumpIn` exists (query replies — a missing pump stalls Claude at startup), EIO-as-EOF, slave-fd close after `Start`, Setsid+Setctty for group kill, job object on Windows, `exec` in `shellArgv`, the process-global manager and why it is not per-`Service`, the `session` category/mode pairing rule.

`CHANGELOG.md` — under the unreleased heading:

```
- Agent sessions core (no UI yet): gg can run interactive agents in embedded
  pseudo-terminals (`internal/agentsession`), with a new `session` external-tool
  category (Claude/Codex/Junie/Antigravity/Kimi + opt-in yolo variants) and a
  first-run auto-configure that writes the safe entries for detected agents to
  the global config. The TUI console arrives in the next stage.
```

- [ ] **Step 2: Full gate**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && ./test.sh race`
Expected: vet+gofmt clean, all unit tests and e2e PASS.

- [ ] **Step 3: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions && git add CHANGELOG.md CLAUDE.md docs/CLAUDE-details.md && git commit -m "docs: agent sessions core (agentsession package, session category)" -m "Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43"
```

- [ ] **Step 4: Hand back** — report the gate result; ask before merging (merge discipline: `--no-ff`, user approves). Plan 2 (TUI) is written next, against Task 6's *Produces* surface.
