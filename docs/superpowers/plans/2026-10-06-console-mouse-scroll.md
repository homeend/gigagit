# Agent console mouse pass-through, scroll mode & selection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids implementer subagents — CLAUDE.md "Workflow"). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wheel, drag and copy work in the TUI agent console: forwarded to programs that track the mouse (fullscreen Claude, mc, their OSC 52 copies reach the clipboard), and a gg-owned frozen scroll mode with line and character selection for normal-screen programs (normal Claude, Junie, shells).

**Architecture:** `agentsession` gains mode tracking (x/vt `EnableMode`/`DisableMode` callbacks), `SendMouse`, OSC 52 capture in the existing `oscFilter`, and a cheap frozen `History` snapshot. `domain` only aliases the new types. The TUI gets a mouse router (`console_mouse.go`) ahead of `handleMouse`'s press-only gate, and a scroll mode (`console_scroll.go`) living on `consoleState` that `renderConsole` and `updateConsoleKey` consult first.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10 (`tea.MouseMsg`), `charmbracelet/x/vt` + `ultraviolet` (`uv.Line`, `uv.Mouse*Event`), `x/ansi` modes.

**Spec:** `docs/superpowers/specs/2026-10-06-console-mouse-scroll-design.md`

## Global Constraints

- Work only in `/work/gigagit/.claude/worktrees/console-mouse-scroll` on `feat/console-mouse-scroll`; every shell command starts with `cd /work/gigagit/.claude/worktrees/console-mouse-scroll &&` (the shell cwd resets).
- No implementer subagents; this session executes every task.
- `internal/tui` never imports `internal/agentsession` in non-test code — use the `domain` aliases (archtest). Test files may (they already do).
- Every user-visible TUI string goes through `i18n.T` with a literal key present in `ja.toml`, `ko.toml`, `ru.toml`, `zh.toml` (follow the `adding-translations` skill); reuse `"Copied %d lines"`.
- New tests call `t.Parallel()` unless they use `startTestSession` (global session manager → serial).
- OSC 52 cap: 1 MiB of base64 payload (`clipCap = 1 << 20`). Clipboard *read* requests (`?`) are ignored.
- Wheel step: `m.wheelStep()` rows per notch for gg scrolling; alternate scroll sends 3 arrow keys per notch.
- Double/triple click window: 400 ms, same cell.
- Selection paint: AttrReverse on the selected cells; scroll-mode cursor row: AttrUnderline on the row (cell-level — the rows are the child's own ANSI, so lipgloss styles cannot be layered on top).
- Commit after each task with the session trailer:
  ```
  Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16
  ```
- Never `git add -A` (a built `bin/gg` may sit in the worktree); add paths explicitly.

## Review Focus

1. **A drag that leaves the console box** (release over the left panels) — the program must still get the release, or fullscreen Claude stays mid-selection. Test: `TestConsoleForwardsReleaseOutsideBox` (Task 3).
2. **esc in scroll mode on a focused Claude console** — must never reach the agent (it interrupts Claude). Test: `TestScrollEscNeverReachesAgent` (Task 6).
3. **The PTY a different size from the box** (another viewer resized it, or the box is narrower than the 20-col PTY minimum) — clicks must clamp, not send coordinates the program cannot place. Test: `TestConsoleMouseClampsToEmulator` (Task 3).
4. **Wheel while the console is unfocused and the left panel has focus** — scrolls the console, focus stays on the left panel. Test: `TestConsoleWheelHoverKeepsFocus` (Task 3) and `TestScrollWheelEntersOnUnfocusedConsole` (Task 6).
5. **A selection copied from a row that holds wide glyphs** (CJK, emoji) — no stray spaces. Test: `TestHistoryTextSkipsWideHalves` (Task 5).

---

## Stage 1 — pass-through + OSC 52

### Task 1: `agentsession` — input modes and `SendMouse`

**Files:**
- Modify: `internal/agentsession/session.go` (struct field, `SetCallbacks`)
- Modify: `internal/agentsession/io.go` (new methods)
- Modify: `internal/agentsession/types.go` (new types)
- Test: `internal/agentsession/mouse_test.go` (new)

**Interfaces:**
- Produces:
  - `type Mouse = uv.MouseEvent`
  - `type InputModes struct { Mouse, AltScreen bool; Cols, Rows int }`
  - `func (s *Session) Input() InputModes`
  - `func (s *Session) SendMouse(ev Mouse)` — no-op unless running; touches `LastUsed`, not `lastIn`.

- [ ] **Step 1: Write the failing tests**

`internal/agentsession/mouse_test.go`:

```go
package agentsession

import (
	"runtime"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

func waitText(t *testing.T, s *Session, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.screenText(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, s.screenText())
}

func TestInputModesFollowMouseTracking(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m1", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`printf 'A\n'; sleep 0.3; printf '\033[?1000h\033[?1006hB\n'; sleep 0.3; printf '\033[?1049hC'; sleep 0.3; printf '\033[?1000l\033[?1049lD\n'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
	waitText(t, s, "A")
	if m := s.Input(); m.Mouse || m.AltScreen {
		t.Fatalf("at start: %+v", m)
	}
	waitText(t, s, "B")
	if m := s.Input(); !m.Mouse || m.AltScreen || m.Cols != 40 || m.Rows != 5 {
		t.Fatalf("after ?1000h: %+v", m)
	}
	waitText(t, s, "C")
	if m := s.Input(); !m.Mouse || !m.AltScreen {
		t.Fatalf("after ?1049h: %+v", m)
	}
	waitText(t, s, "D")
	if m := s.Input(); m.Mouse || m.AltScreen {
		t.Fatalf("after reset: %+v", m)
	}
}

// The child turns SGR tracking on, then prints in hex the bytes it reads.
func TestSendMouseReachesChildAsSGR(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m2", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`stty raw -echo; printf '\033[?1000h\033[?1006hREADY'; head -c 9 | od -An -tx1 | tr -d ' \n'; printf 'END'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
	waitText(t, s, "READY")
	s.SendMouse(uv.MouseClickEvent{X: 2, Y: 3, Button: uv.MouseLeft})
	// ESC [ < 0 ; 3 ; 4 M — x/ansi adds 1 to both coordinates.
	waitText(t, s, "1b5b3c303b333b344dEND")
}

func TestSendMouseIsInertWithoutTracking(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m3", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`stty raw -echo; printf 'READY'; head -c 1 | od -An -tx1 | tr -d ' \n'; printf 'END'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
	waitText(t, s, "READY")
	s.SendMouse(uv.MouseClickEvent{X: 2, Y: 3, Button: uv.MouseLeft})
	s.SendText("z")
	waitText(t, s, "7aEND") // only the z arrived: the click produced no bytes
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/agentsession -run 'TestInputModes|TestSendMouse' -count=1`
Expected: build failure — `s.Input undefined`, `s.SendMouse undefined`.

- [ ] **Step 3: Implement**

`types.go` — add after `Key`:

```go
// Mouse is one mouse event for SendMouse (click, release, wheel, motion),
// encoded by the emulator in the mouse mode the child enabled.
type Mouse = uv.MouseEvent

// InputModes is what the child asked of its terminal's input: mouse
// tracking (any of modes 9/1000/1001/1002/1003) and the alternate screen,
// plus the emulator's size for clamping coordinates.
type InputModes struct {
	Mouse, AltScreen bool
	Cols, Rows       int
}
```

`session.go` — field next to `cursorHidden`:

```go
	mouseModes atomic.Uint32 // bit per mouse-tracking DEC mode the child set (mouseModeBit)
```

and replace the `SetCallbacks` line in `start`:

```go
	// Callbacks run inside emu.Write under the emulator's lock: store only.
	emu.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(v bool) { s.cursorHidden.Store(!v) },
		EnableMode:       func(m ansi.Mode) { s.setMouseMode(m, true) },
		DisableMode:      func(m ansi.Mode) { s.setMouseMode(m, false) },
	})
```

with the import `"github.com/charmbracelet/x/ansi"` and, below `start`:

```go
// mouseModeBit maps the mouse-tracking DEC modes to a bit each; any set bit
// means the child reads mouse reports.
func mouseModeBit(m ansi.Mode) uint32 {
	switch m {
	case ansi.ModeMouseX10:
		return 1
	case ansi.ModeMouseNormal:
		return 2
	case ansi.ModeMouseHighlight:
		return 4
	case ansi.ModeMouseButtonEvent:
		return 8
	case ansi.ModeMouseAnyEvent:
		return 16
	}
	return 0
}

func (s *Session) setMouseMode(m ansi.Mode, on bool) {
	bit := mouseModeBit(m)
	if bit == 0 {
		return
	}
	for {
		old := s.mouseModes.Load()
		next := old &^ bit
		if on {
			next = old | bit
		}
		if s.mouseModes.CompareAndSwap(old, next) {
			return
		}
	}
}
```

`io.go` — add after `Paste`:

```go
// Input reports the child's input modes and the emulator size.
func (s *Session) Input() InputModes {
	return InputModes{
		Mouse:     s.mouseModes.Load() != 0,
		AltScreen: s.emu.IsAltScreen(),
		Cols:      s.emu.Width(),
		Rows:      s.emu.Height(),
	}
}

// SendMouse encodes ev for the child in the mouse mode it enabled (a no-op
// when it enabled none, or once the session has exited). Coordinates are
// 0-based cells of the emulator. A mouse event is not typed input: it
// touches LastUsed but leaves LastInput alone.
func (s *Session) SendMouse(ev Mouse) {
	if !s.running() {
		return
	}
	s.Touch()
	s.withEmu(func() { s.emu.SendMouse(ev) })
}
```

If `ansi.Mode` values are not comparable with `==` against `ansi.ModeMouseX10` (compile error), switch on `m.(ansi.DECMode)` instead: `dm, ok := m.(ansi.DECMode); if !ok { return 0 }` then `switch dm`.

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/agentsession -count=1`
Expected: PASS (all agentsession tests).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add internal/agentsession/session.go internal/agentsession/io.go internal/agentsession/types.go internal/agentsession/mouse_test.go && git commit -m "feat(agentsession): input modes and SendMouse for the console's mouse pass-through

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

### Task 2: `agentsession` — OSC 52 capture

**Files:**
- Modify: `internal/agentsession/oscfilter.go`
- Modify: `internal/agentsession/session.go` (`pumpOut`, fields)
- Modify: `internal/agentsession/types.go` (`Clip`)
- Test: `internal/agentsession/oscfilter_test.go`, `internal/agentsession/mouse_test.go`

**Interfaces:**
- Produces:
  - `type Clip struct { Text string; Seq int; Over bool }` — the latest OSC 52 write; `Seq` counts writes (0 = none); `Over` = the latest was over the cap and dropped (Text "").
  - `func (s *Session) Clipboard() Clip`
  - `const clipCap = 1 << 20`

- [ ] **Step 1: Write the failing tests**

Append to `oscfilter_test.go`:

```go
func TestOSCFilterCapturesClipboardWrites(t *testing.T) {
	t.Parallel()
	var f oscFilter
	// "hello\nworld" base64, split across two reads, BEL-terminated.
	f.filter([]byte("x\x1b]52;c;aGVsbG8K"))
	f.filter([]byte("d29ybGQ=\x07y"))
	if !f.clipChanged || f.clip != "hello\nworld" || f.clipSeq != 1 || f.clipOver {
		t.Fatalf("got changed=%v clip=%q seq=%d over=%v", f.clipChanged, f.clip, f.clipSeq, f.clipOver)
	}
	f.clipChanged = false
	f.filter([]byte("\x1b]52;;Zm9v\x1b\\")) // empty selection param, ST-terminated
	if !f.clipChanged || f.clip != "foo" || f.clipSeq != 2 {
		t.Fatalf("ST form: clip=%q seq=%d", f.clip, f.clipSeq)
	}
}

func TestOSCFilterIgnoresClipboardReadsAndJunk(t *testing.T) {
	t.Parallel()
	var f oscFilter
	f.filter([]byte("\x1b]52;c;?\x07"))     // a read request: never answered, never stored
	f.filter([]byte("\x1b]52;c;!!!\x07"))   // not base64
	f.filter([]byte("\x1b]52;c\x07"))       // no data field
	if f.clipChanged || f.clipSeq != 0 {
		t.Fatalf("stored something: seq=%d clip=%q", f.clipSeq, f.clip)
	}
}

func TestOSCFilterDropsOversizedClipboard(t *testing.T) {
	t.Parallel()
	var f oscFilter
	big := strings.Repeat("QUFB", clipCap/4+10) // > clipCap base64 bytes
	f.filter([]byte("\x1b]52;c;" + big + "\x07"))
	if !f.clipChanged || !f.clipOver || f.clip != "" || f.clipSeq != 1 {
		t.Fatalf("over=%v clip len=%d seq=%d", f.clipOver, len(f.clip), f.clipSeq)
	}
	f.clipChanged = false
	f.filter([]byte("\x1b]52;c;Zm9v\x07")) // the next small one is fine again
	if f.clipOver || f.clip != "foo" {
		t.Fatalf("after oversize: over=%v clip=%q", f.clipOver, f.clip)
	}
}

func TestOSCFilterClipboardStaysOffScreen(t *testing.T) {
	t.Parallel()
	got := screenOf(40, 2, []byte("\x1b]52;c;aGVsbG8=\x07AB"))
	if got[0] != "AB" {
		t.Fatalf("row0 = %q", got[0])
	}
}
```

Append to `mouse_test.go`:

```go
func TestSessionClipboardFromChild(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("c1", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`printf '\033]52;c;Y29waWVk\007DONE'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
	waitText(t, s, "DONE")
	if c := s.Clipboard(); c.Text != "copied" || c.Seq != 1 || c.Over {
		t.Fatalf("clip = %+v", c)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/agentsession -run 'Clipboard|ClipboardWrites|ClipboardReads|Oversized' -count=1`
Expected: build failure — `f.clipChanged undefined`, `clipCap undefined`, `s.Clipboard undefined`.

- [ ] **Step 3: Implement**

`oscfilter.go`:
- add to the `oscFilter` struct:

```go
	clipBuf     []byte // the OSC 52 payload read so far (capped at clipCap)
	clipLong    bool   // the payload being read passed clipCap
	clip        string // the last decoded clipboard write
	clipSeq     int    // clipboard writes committed (an over-cap one included)
	clipOver    bool   // the last write was over clipCap and dropped
	clipChanged bool   // a clipboard write committed since the reader last cleared it
```

- add `oscClip` to the `oscKind` constants (`oscClip // OSC 52: a clipboard write`), and the cap:

```go
// clipCap bounds an OSC 52 payload (base64 bytes); a longer one is dropped.
const clipCap = 1 << 20
```

- in `record`, the `oscNumber` `;` switch gains `case "52": f.osc, f.clipBuf, f.clipLong = oscClip, f.clipBuf[:0], false`, and a new case:

```go
	case oscClip:
		if len(f.clipBuf) < clipCap {
			f.clipBuf = append(f.clipBuf, b)
		} else {
			f.clipLong = true
		}
```

- in `commit`, a new case:

```go
	case oscClip:
		f.commitClip()
```

with

```go
// commitClip stores an OSC 52 write: "<selection>;<base64>". A read request
// ("?") and undecodable data are ignored; an over-cap payload is recorded as
// dropped so the frontend can say so.
func (f *oscFilter) commitClip() {
	if f.clipLong {
		f.clip, f.clipOver = "", true
		f.clipSeq++
		f.clipChanged = true
		return
	}
	_, data, ok := strings.Cut(string(f.clipBuf), ";")
	if !ok || data == "?" {
		return
	}
	dec, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return
	}
	f.clip, f.clipOver = string(dec), false
	f.clipSeq++
	f.clipChanged = true
}
```

(import `encoding/base64`). Note `commit` already sets `f.osc = oscNone` after the switch.

`types.go`:

```go
// Clip is the last clipboard write the child asked for (OSC 52). Seq counts
// writes (0 = none yet); Over = the last was over the cap and dropped.
type Clip struct {
	Text string
	Seq  int
	Over bool
}
```

`session.go` — field next to `sig`: `clip Clip // the last OSC 52 write; under sigMu`. In `pumpOut`, after the `if s.osc.changed { … }` block:

```go
			if s.osc.clipChanged {
				s.osc.clipChanged = false
				s.sigMu.Lock()
				s.clip = Clip{Text: s.osc.clip, Seq: s.osc.clipSeq, Over: s.osc.clipOver}
				s.sigMu.Unlock()
			}
```

and the accessor after `Signals`:

```go
// Clipboard is the child's last OSC 52 clipboard write.
func (s *Session) Clipboard() Clip {
	s.sigMu.Lock()
	defer s.sigMu.Unlock()
	return s.clip
}
```

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/agentsession -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add internal/agentsession && git commit -m "feat(agentsession): capture the child's OSC 52 clipboard writes

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

### Task 3: TUI mouse router — pass-through and alternate scroll

**Files:**
- Modify: `internal/domain/sessions.go` (aliases)
- Create: `internal/tui/console_mouse.go`
- Modify: `internal/tui/console.go` (`consoleState` fields)
- Modify: `internal/tui/mouse.go` (call the router before the press-only gate; old console branch stays as the fallback for events the router declines)
- Test: `internal/tui/console_mouse_test.go` (new)

**Interfaces:**
- Consumes: `Session.Input()`, `Session.SendMouse(ev)`, `Session.SendKey` (Task 1).
- Produces:
  - domain aliases `SessionMouse = agentsession.Mouse`, `SessionInputModes = agentsession.InputModes`, `SessionClip = agentsession.Clip`
  - `func (m Model) consoleRect() (x, y, w, h int)` — the console box on screen.
  - `func (m Model) consoleCell(x, y int) (cx, cy int, inBox, inContent bool)`
  - `func (m Model) consoleMouse(msg tea.MouseMsg) (Model, tea.Cmd, bool)` — true = handled.
  - `func consoleMouseEvent(msg tea.MouseMsg, x, y int) (domain.SessionMouse, bool)`
  - `func (m Model) focusConsoleByClick() Model`
  - `consoleState.held tea.MouseButton` — a forwarded button still down (`tea.MouseButtonNone` = none).

- [ ] **Step 1: Write the failing tests**

`internal/tui/console_mouse_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// mouseAt sends one mouse message through Update.
func mouseAt(m Model, x, y int, b tea.MouseButton, a tea.MouseAction) Model {
	nm, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: a})
	return nm.(Model)
}

// contentOrigin is the screen cell of the console's first content cell.
func contentOrigin(m Model) (int, int) {
	x, y, _, _ := m.consoleRect()
	return x + 2, y + 2
}

// A child with SGR tracking on prints the hex of what it reads.
const sgrEcho = `stty raw -echo; printf '\033[?1000h\033[?1002h\033[?1006hREADY'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`

func TestConsoleForwardsClickToTrackingChild(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, sgrEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+2, y0+3, tea.MouseButtonLeft, tea.MouseActionPress)
	waitScreen(t, s, "1b5b3c303b333b344d") // ESC[<0;3;4M
	m = mouseAt(m, x0+2, y0+3, tea.MouseButtonLeft, tea.MouseActionRelease)
	waitScreen(t, s, "1b5b3c303b333b346d") // ESC[<0;3;4m
}

func TestConsoleForwardsReleaseOutsideBox(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, sgrEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+1, y0+1, tea.MouseButtonLeft, tea.MouseActionPress)
	// Released over the left panels: still reaches the child, clamped to col 0.
	m = mouseAt(m, 1, y0+1, tea.MouseButtonLeft, tea.MouseActionRelease)
	waitScreen(t, s, "1b5b3c303b313b326d") // ESC[<0;1;2m
	if m.console.held != tea.MouseButtonNone {
		t.Fatal("held button not cleared on release")
	}
}

func TestConsoleMouseClampsToEmulator(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, sgrEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	in := s.Input()
	_ = s.Resize(in.Cols-10, in.Rows) // another viewer shrank the PTY
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+in.Cols-2, y0, tea.MouseButtonLeft, tea.MouseActionPress)
	cols := in.Cols - 10
	// ESC [ < 0 ; <cols> ; 1 M — the last column the PTY has.
	waitScreen(t, s, hexOf("\x1b[<0;"+itoa(cols)+";1M"))
}

func TestConsoleWheelHoverKeepsFocus(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, sgrEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	m.console.focused = false
	m.focus = panelBranches
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+4, y0+2, tea.MouseButtonWheelUp, tea.MouseActionPress)
	waitScreen(t, s, hexOf("\x1b[<64;5;3M"))
	if m.focus != panelBranches || m.console.focused {
		t.Fatalf("wheel moved focus: focus=%v consoleFocused=%v", m.focus, m.console.focused)
	}
}

func TestConsoleAltScreenWheelSendsArrows(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `stty raw -echo; printf '\033[?1049hREADY'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelUp, tea.MouseActionPress)
	waitScreen(t, s, strings.Repeat(hexOf("\x1b[A"), 3))
}
```

Helpers (add to the same test file):

```go
func hexOf(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteString(strings.ToLower(strconv.FormatInt(int64(s[i])|0x100, 16)[1:]))
	}
	return b.String()
}

func itoa(i int) string { return strconv.Itoa(i) }
```

(import `strconv`). If `itoa` already exists in package `tui` tests, drop this one and use `strconv.Itoa` directly.

- [ ] **Step 2: Run them to see them fail**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui -run 'TestConsole(Forwards|MouseClamps|WheelHover|AltScreenWheel)' -count=1`
Expected: build failure — `m.consoleRect undefined`, `m.console.held undefined`.

- [ ] **Step 3: Implement**

`internal/domain/sessions.go` — extend the alias block:

```go
	// The console's mouse pass-through and the child's clipboard writes.
	SessionMouse      = agentsession.Mouse
	SessionInputModes = agentsession.InputModes
	SessionClip       = agentsession.Clip
```

`internal/tui/console.go` — `consoleState` gains:

```go
	held tea.MouseButton // a button forwarded to the child and not yet released (MouseButtonNone = none)
```

`internal/tui/mouse.go` — in `handleMouse`, as the very first statement (before `if msg.Action != tea.MouseActionPress`):

```go
	// The agent console takes press, motion and release over its box (and a
	// drag it started, wherever it goes) before the press-only gate below.
	if nm, cmd, ok := m.consoleMouse(msg); ok {
		return nm, cmd
	}
```

and replace the body of the existing console focus branch's left-click case with `return m.focusConsoleByClick(), nil` (behaviour unchanged; the router now reaches it first for in-box presses, the branch remains for presses the router declines, e.g. on a gone session).

`internal/tui/console_mouse.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/homeend/gigagit/internal/domain"
)

// consoleRect is the console box on screen: the whole body when maximised
// (below the header row), else the Commits column's box.
func (m Model) consoleRect() (x, y, w, h int) {
	w, h = m.consoleBox()
	if m.console != nil && m.console.maximized {
		return 0, 1, w, h
	}
	p := m.layout().pos[panelCommits]
	return p.x, p.y, w, h
}

// consoleCell maps a screen cell to the console's content area: border and
// padding across, border and the title row down (consoleInner's geometry).
func (m Model) consoleCell(x, y int) (cx, cy int, inBox, inContent bool) {
	bx, by, w, h := m.consoleRect()
	inBox = x >= bx && x < bx+w && y >= by && y < by+h
	cols, rows := consoleInner(w, h)
	cx, cy = x-bx-2, y-by-2
	inContent = inBox && cx >= 0 && cx < cols && cy >= 0 && cy < rows
	return cx, cy, inBox, inContent
}

// consoleMouseOwner reports whether the console may take the mouse at all:
// nothing is layered above it (the keyboard's precedence in updateConsoleKey).
func (m Model) consoleMouseOwner() bool {
	return m.console != nil && m.modal == nil && m.proc == nil && m.actionMenu == nil && m.topLayer() == nil
}

// focusConsoleByClick is a left click's focus move onto the console: what
// enter does on the unfocused console.
func (m Model) focusConsoleByClick() Model {
	if m.console.focused && m.focus == panelCommits {
		return m
	}
	m.filterTyping = false
	m = m.rememberLeftFocus()
	m.focus = panelCommits
	if m.filesView != nil {
		m = m.focusRight()
	}
	m.console.focused = true
	m.touchConsole()
	return m.syncConsoleSize() // gaining focus takes the size back
}

// consoleMouse routes a mouse event over the console by the child's live
// modes (spec §1). false = not the console's: handleMouse goes on.
func (m Model) consoleMouse(msg tea.MouseMsg) (Model, tea.Cmd, bool) {
	if !m.consoleMouseOwner() {
		return m, nil, false
	}
	cx, cy, inBox, inContent := m.consoleCell(msg.X, msg.Y)
	held := m.console.held != tea.MouseButtonNone
	if !inBox && !held {
		return m, nil, false
	}
	sess, ok := m.consoleSession()
	if !ok {
		return m, nil, false // the gone-session console: the old branch swallows
	}
	running := sess.Info().State == domain.SessionRunning
	modes := sess.Input()
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && inBox {
		m = m.focusConsoleByClick()
	}
	switch {
	case running && modes.Mouse:
		if !inContent && !held && msg.Action == tea.MouseActionPress {
			return m, nil, true // a press on the border or title only focuses
		}
		ev, ok := consoleMouseEvent(msg, clampInt(cx, 0, modes.Cols-1), clampInt(cy, 0, modes.Rows-1))
		if ok {
			sess.SendMouse(ev)
		}
		switch {
		case msg.Action == tea.MouseActionRelease:
			m.console.held = tea.MouseButtonNone
		case msg.Action == tea.MouseActionPress && !isWheel(msg.Button):
			m.console.held = msg.Button
		}
		return m, nil, true
	case running && modes.AltScreen:
		m.console.held = tea.MouseButtonNone
		if k, ok := wheelArrow(msg.Button); ok {
			for range 3 {
				sess.SendKey(k)
			}
		}
		return m, nil, true
	}
	m.console.held = tea.MouseButtonNone
	return m, nil, true // normal screen: the scroll mode router (Task 6) goes here
}

func isWheel(b tea.MouseButton) bool {
	return b == tea.MouseButtonWheelUp || b == tea.MouseButtonWheelDown ||
		b == tea.MouseButtonWheelLeft || b == tea.MouseButtonWheelRight
}

// wheelArrow is xterm's alternate scroll: a wheel notch as a cursor key.
func wheelArrow(b tea.MouseButton) (domain.SessionKey, bool) {
	switch b {
	case tea.MouseButtonWheelUp:
		return uv.KeyPressEvent{Code: uv.KeyUp}, true
	case tea.MouseButtonWheelDown:
		return uv.KeyPressEvent{Code: uv.KeyDown}, true
	}
	return nil, false
}

var teaToUVButton = map[tea.MouseButton]uv.MouseButton{
	tea.MouseButtonNone: uv.MouseNone, tea.MouseButtonLeft: uv.MouseLeft,
	tea.MouseButtonMiddle: uv.MouseMiddle, tea.MouseButtonRight: uv.MouseRight,
	tea.MouseButtonWheelUp: uv.MouseWheelUp, tea.MouseButtonWheelDown: uv.MouseWheelDown,
	tea.MouseButtonWheelLeft: uv.MouseWheelLeft, tea.MouseButtonWheelRight: uv.MouseWheelRight,
	tea.MouseButtonBackward: uv.MouseBackward, tea.MouseButtonForward: uv.MouseForward,
}

// consoleMouseEvent turns a Bubble Tea mouse message into the emulator's
// event at content cell (x, y).
func consoleMouseEvent(msg tea.MouseMsg, x, y int) (domain.SessionMouse, bool) {
	b, ok := teaToUVButton[msg.Button]
	if !ok {
		return nil, false
	}
	var mod uv.KeyMod
	if msg.Shift {
		mod |= uv.ModShift
	}
	if msg.Alt {
		mod |= uv.ModAlt
	}
	if msg.Ctrl {
		mod |= uv.ModCtrl
	}
	mm := uv.Mouse{X: x, Y: y, Button: b, Mod: mod}
	switch {
	case msg.Action == tea.MouseActionPress && isWheel(msg.Button):
		return uv.MouseWheelEvent(mm), true
	case msg.Action == tea.MouseActionPress:
		return uv.MouseClickEvent(mm), true
	case msg.Action == tea.MouseActionRelease:
		return uv.MouseReleaseEvent(mm), true
	case msg.Action == tea.MouseActionMotion:
		return uv.MouseMotionEvent(mm), true
	}
	return nil, false
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return max(lo, min(v, hi))
}
```

If `clampInt` already exists in package `tui`, use it and drop this copy (`grep -n "func clampInt" internal/tui/*.go`). The focus branch in `mouse.go` that remains must keep its maximised `return m, nil`.

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui -run 'Console|Mouse' -count=1 && go test ./internal/archtest -count=1`
Expected: PASS (new tests plus the existing console/mouse tests, notably the click-to-focus ones in `console_test.go`).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add internal/domain/sessions.go internal/tui/console_mouse.go internal/tui/console_mouse_test.go internal/tui/console.go internal/tui/mouse.go && git commit -m "feat(tui): the console forwards the mouse to programs that track it

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

### Task 4: TUI — the child's OSC 52 copies reach the clipboard; stage-1 docs

**Files:**
- Modify: `internal/tui/console.go` (`consoleState.clipSeq`, set when shown; `consumeConsoleClip`)
- Modify: `internal/tui/model.go` (`consoleChangedMsg` case)
- Modify: `internal/tui/help.go`, `internal/tui/footer.go` (mention the mouse)
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`
- Modify: `CHANGELOG.md`, `README.md`
- Test: `internal/tui/console_mouse_test.go`

**Interfaces:**
- Consumes: `Session.Clipboard() Clip` (Task 2), `copyToClipboardCmd`.
- Produces: `func (m Model) consumeConsoleClip() (Model, tea.Cmd)`; `consoleState.clipSeq int`.

- [ ] **Step 1: Write the failing tests**

Append to `console_mouse_test.go`:

```go
func TestConsoleOSC52GoesToClipboard(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, `printf '\033]52;c;b25lCnR3bw==\007OLD'; read _; printf '\033]52;c;bmV3\007NEW'; sleep 5`)
	waitScreen(t, s, "OLD")
	m, _ = m.openConsole(s.Info().ID) // a copy made before the console showed is not replayed
	nm, cmd := m.consumeConsoleClip()
	if cmd != nil {
		t.Fatal("a stale clipboard write was replayed")
	}
	m = nm
	s.SendText("\r")
	waitScreen(t, s, "NEW")
	m, cmd = m.consumeConsoleClip()
	if cmd == nil {
		t.Fatal("no copy command for the new OSC 52 write")
	}
	msg := cmd().(clipboardCopiedMsg)
	if copied != "new" || msg.err != nil || msg.ok == "" {
		t.Fatalf("copied=%q msg=%+v", copied, msg)
	}
}
```

(import `io`).

- [ ] **Step 2: Run it to see it fail**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui -run TestConsoleOSC52 -count=1`
Expected: build failure — `m.consumeConsoleClip undefined`.

- [ ] **Step 3: Implement**

`console.go`: `consoleState` gains `clipSeq int // the child's last OSC 52 write this console has taken`. In `showConsole`, where the new `consoleState` is built for the session, set `clipSeq: sess.Clipboard().Seq` (look the session up the way `showConsole` already does; if it has no session handle there, set it right after `m.console` is assigned: `if s, ok := m.consoleSession(); ok { m.console.clipSeq = s.Clipboard().Seq }`). Then:

```go
// consumeConsoleClip copies the child's newest OSC 52 write (a fullscreen
// agent's own selection) through the TUI's one clipboard writer. Only the
// shown console's session is read: a copy needs the user's mouse or keys in
// that console.
func (m Model) consumeConsoleClip() (Model, tea.Cmd) {
	s, ok := m.consoleSession()
	if !ok {
		return m, nil
	}
	c := s.Clipboard()
	if c.Seq == m.console.clipSeq {
		return m, nil
	}
	m.console.clipSeq = c.Seq
	if c.Over {
		m.statusMsg = i18n.T("copy too large — dropped")
		return m, nil
	}
	return m, m.copyToClipboardCmd(i18n.T("Copied %d lines", strings.Count(c.Text, "\n")+1), c.Text)
}
```

`model.go` — the `consoleChangedMsg` case becomes:

```go
		m, clipCmd := m.consumeConsoleClip()
		return m, tea.Batch(clipCmd, waitSessionCmd(m.console, msg.id, msg.gen))
```

(keep the two early-return guards above it).

Help (`help.go`, next to the `ctrl+]` row): add

```go
		r(i18n.T("mouse"), i18n.T("over an agent console the mouse goes to the program when it asks for it (Claude Code's fullscreen mode, mc): the wheel scrolls it, a drag selects, and what it copies reaches the clipboard; a full-screen program without mouse support gets ↑/↓ for the wheel")),
```

Footer (`footer.go:266`, the focused-console line): change the key to `"agent console: every key goes to the agent, the mouse too when it asks  [%s] step out  [%s] sessions"`.

i18n: add both new help strings, the new footer string (remove the old footer key from all four bundles if nothing else uses it — `grep -rn '"agent console: every key goes to the agent  \[' internal/tui`), `"mouse"`, and `"copy too large — dropped"` to all four bundles per the `adding-translations` skill.

CHANGELOG (top, Unreleased → Added): "Agent console: the mouse reaches programs that ask for it — Claude Code's fullscreen mode scrolls with the wheel and its drag-selections land on the clipboard (OSC 52); mc and other mouse-aware programs get clicks, drags and the wheel; full-screen programs without mouse support scroll with ↑/↓." README: one sentence in the agent console section saying the same.

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui ./internal/i18n -count=1`
Expected: PASS, including the i18n AST gates (`i18n_scan_test.go` & co).

- [ ] **Step 5: Manual check (stage 1)**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go build -o bin/gg ./cmd/gg`, then tell the user to run `/work/gigagit/.claude/worktrees/console-mouse-scroll/bin/gg` from a repo, open a Claude console (fullscreen setting on), and check: wheel scrolls Claude's transcript while the left panel keeps focus; a drag selects inside Claude and the text pastes elsewhere; mc in Open terminal takes clicks and the wheel.

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add internal/tui/console.go internal/tui/model.go internal/tui/help.go internal/tui/footer.go internal/tui/console_mouse_test.go internal/i18n/lang CHANGELOG.md README.md && git commit -m "feat(tui): an agent's OSC 52 copies reach the clipboard

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

---

## Stage 2 — scroll mode + selection

### Task 5: `agentsession` — the frozen `History` snapshot

**Files:**
- Create: `internal/agentsession/history.go`
- Test: `internal/agentsession/history_test.go`
- Modify: `internal/domain/sessions.go` (aliases)

**Interfaces:**
- Produces:
  - `type History struct` (unexported fields `lines []uv.Line`, `width int`, `taken time.Time`)
  - `func (s *Session) History() History`
  - `func (h History) Len() int`, `func (h History) Width() int`, `func (h History) Taken() time.Time`
  - `type RowMarks struct { SelFrom, SelTo int; Cursor bool }` — cells `SelFrom..SelTo-1` reversed (none when `SelFrom >= SelTo`); `Cursor` underlines the row.
  - `func (h History) Row(i int, mk RowMarks) string` — ANSI, exactly `Width()` cells.
  - `func (h History) Text(r0, c0, r1, c1 int) string` — stream selection, inclusive ends, either order.
  - `func (h History) WordAt(r, c int) (c0, c1 int)` — inclusive bounds of the non-blank run at (r, c); `c0 > c1` on a blank.
  - `func (h History) RowWidth(r int) int` — columns up to the last non-blank cell (0 for a blank row).
  - domain aliases `SessionHistory = agentsession.History`, `SessionRowMarks = agentsession.RowMarks`.

- [ ] **Step 1: Write the failing tests**

`internal/agentsession/history_test.go`:

```go
package agentsession

import (
	"runtime"
	"strings"
	"testing"
)

func historySession(t *testing.T, script string, cols, rows int) *Session {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("h", StartSpec{Dir: t.TempDir(), Cols: cols, Rows: rows, Argv: []string{"sh", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
	return s
}

func TestHistoryHoldsScrollbackThenScreen(t *testing.T) {
	t.Parallel()
	s := historySession(t, `i=0; while [ $i -lt 30 ]; do echo "line $i"; i=$((i+1)); done; printf 'END'; sleep 5`, 20, 5)
	waitText(t, s, "END")
	h := s.History()
	if h.Len() != 31 || h.Width() != 20 {
		t.Fatalf("len=%d width=%d", h.Len(), h.Width())
	}
	if got := h.Text(0, 0, 0, 19); got != "line 0" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := h.Text(30, 0, 30, 19); got != "END" {
		t.Fatalf("last row = %q", got)
	}
}

func TestHistoryStaysFrozen(t *testing.T) {
	t.Parallel()
	s := historySession(t, `echo first; read _; i=0; while [ $i -lt 20 ]; do echo "more $i"; i=$((i+1)); done; printf 'END'; sleep 5`, 20, 5)
	waitText(t, s, "first")
	h := s.History()
	before := h.Text(0, 0, h.Len()-1, 19)
	taken := h.Taken()
	s.SendText("\r")
	waitText(t, s, "END")
	if after := h.Text(0, 0, h.Len()-1, 19); after != before {
		t.Fatalf("snapshot changed:\n%q\n%q", before, after)
	}
	if !s.LastOutput().After(taken) {
		t.Fatal("LastOutput did not pass the snapshot time")
	}
}

func TestHistoryTextStreamAndTrim(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf 'abc   \r\ndefgh\r\nij'; sleep 5`, 20, 5)
	waitText(t, s, "ij")
	h := s.History()
	if got := h.Text(0, 1, 2, 0); got != "bc\ndefgh\ni" {
		t.Fatalf("stream = %q", got)
	}
	if got := h.Text(2, 0, 0, 1); got != "bc\ndefgh\ni" {
		t.Fatalf("reversed ends = %q", got)
	}
	if h.RowWidth(0) != 3 || h.RowWidth(3) != 0 {
		t.Fatalf("row widths %d %d", h.RowWidth(0), h.RowWidth(3))
	}
}

func TestHistoryTextSkipsWideHalves(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf '漢字ok'; sleep 5`, 20, 5)
	waitText(t, s, "ok")
	h := s.History()
	if got := h.Text(0, 0, 0, 19); got != "漢字ok" {
		t.Fatalf("wide row = %q", got)
	}
	if got := h.Text(0, 1, 0, 2); got != "漢字" { // from a right half to a left half
		t.Fatalf("partial wide = %q", got)
	}
}

func TestHistoryWordAt(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf 'foo bar.baz  q'; sleep 5`, 20, 5)
	waitText(t, s, "q")
	h := s.History()
	if c0, c1 := h.WordAt(0, 5); c0 != 4 || c1 != 10 {
		t.Fatalf("word at 5 = %d..%d", c0, c1)
	}
	if c0, c1 := h.WordAt(0, 11); c0 <= c1 {
		t.Fatalf("blank gave a word %d..%d", c0, c1)
	}
}

func TestHistoryRowMarks(t *testing.T) {
	t.Parallel()
	s := historySession(t, `printf 'hello'; sleep 5`, 10, 5)
	waitText(t, s, "hello")
	h := s.History()
	plain := h.Row(0, RowMarks{})
	sel := h.Row(0, RowMarks{SelFrom: 1, SelTo: 3})
	cur := h.Row(0, RowMarks{Cursor: true})
	if !strings.Contains(sel, "\x1b[7m") || strings.Contains(plain, "\x1b[7m") {
		t.Fatalf("reverse missing/extra: plain=%q sel=%q", plain, sel)
	}
	if !strings.Contains(cur, "\x1b[4m") {
		t.Fatalf("cursor underline missing: %q", cur)
	}
}
```

If the SGR for reverse/underline is emitted combined (e.g. `\x1b[4;7m`), loosen the asserts to check for `7` / `4` inside an SGR via a regexp `\x1b\[[0-9;]*7[0-9;]*m`;.

- [ ] **Step 2: Run them to see them fail**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/agentsession -run TestHistory -count=1`
Expected: build failure — `s.History undefined`.

- [ ] **Step 3: Implement** `internal/agentsession/history.go`:

```go
package agentsession

import (
	"slices"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// History is a frozen copy of what the session printed: its scrollback (the
// main screen's; none in the alt screen) followed by the visible screen.
// Scrollback lines are cloned by the emulator when pushed and never written
// again, so the snapshot shares them and copies only the slice of headers;
// screen rows are cloned. Rows render lazily (Row), a page at a time.
type History struct {
	lines []uv.Line
	width int
	taken time.Time
}

// History snapshots the scrollback and the screen.
func (s *Session) History() History {
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	w, h := s.emu.Width(), s.emu.Height()
	var lines []uv.Line
	if !s.emu.IsAltScreen() {
		if sb := s.emu.Scrollback(); sb != nil {
			lines = slices.Clone(sb.Lines())
		}
	}
	for y := range h {
		row := make(uv.Line, w)
		for x := range w {
			if c := s.emu.CellAt(x, y); c != nil {
				row[x] = *c
			} else {
				row[x] = uv.EmptyCell
			}
		}
		lines = append(lines, row)
	}
	return History{lines: lines, width: w, taken: time.Now()}
}

func (h History) Len() int         { return len(h.lines) }
func (h History) Width() int       { return h.width }
func (h History) Taken() time.Time { return h.taken }

// cell is row r's cell at column c; a blank past a trimmed scrollback line.
func (h History) cell(r, c int) uv.Cell {
	if r < 0 || r >= len(h.lines) || c < 0 || c >= len(h.lines[r]) {
		return uv.EmptyCell
	}
	return h.lines[r][c]
}

func blank(c uv.Cell) bool { return c.Content == "" || c.Content == " " }

// RowMarks highlights one rendered row: cells SelFrom..SelTo-1 reversed
// (a selection), Cursor underlines the whole row (scroll mode's cursor).
type RowMarks struct {
	SelFrom, SelTo int
	Cursor         bool
}

// Row renders row i, exactly Width cells, with its marks.
func (h History) Row(i int, mk RowMarks) string {
	row := make(uv.Line, h.width)
	for c := range h.width {
		row[c] = h.cell(i, c)
		if row[c].IsZero() && (c == 0 || h.cell(i, c-1).Width < 2) {
			row[c] = uv.EmptyCell // a stray zero cell, not a wide glyph's right half
		}
		if row[c].IsZero() {
			continue
		}
		if c >= mk.SelFrom && c < mk.SelTo {
			row[c].Style.Attrs ^= uv.AttrReverse
		}
		if mk.Cursor {
			row[c].Style.Underline = uv.UnderlineSingle
		}
	}
	return row.Render()
}

// RowWidth is the columns up to row r's last non-blank cell.
func (h History) RowWidth(r int) int {
	for c := h.width - 1; c >= 0; c-- {
		if x := h.cell(r, c); !x.IsZero() && !blank(x) {
			return c + 1
		}
	}
	return 0
}

// Text is the stream selection from (r0, c0) to (r1, c1), both ends
// included, in either order: the first row from c0, whole rows between,
// the last row to c1. Cell text only; a wide glyph's right half is skipped
// and a selection that starts on one takes the glyph; trailing blanks are
// trimmed per row; rows join with \n (the emulator keeps no soft-wrap flag).
func (h History) Text(r0, c0, r1, c1 int) string {
	if r1 < r0 || (r1 == r0 && c1 < c0) {
		r0, c0, r1, c1 = r1, c1, r0, c0
	}
	rows := make([]string, 0, r1-r0+1)
	for r := r0; r <= r1; r++ {
		from, to := 0, h.width-1
		if r == r0 {
			from = c0
		}
		if r == r1 {
			to = c1
		}
		if from > 0 && h.cell(r, from).IsZero() && h.cell(r, from-1).Width > 1 {
			from-- // started on a right half: take the glyph
		}
		var b strings.Builder
		for c := from; c <= to && c < h.width; c++ {
			x := h.cell(r, c)
			if x.IsZero() {
				continue
			}
			if x.Content == "" {
				b.WriteByte(' ')
				continue
			}
			b.WriteString(x.Content)
		}
		rows = append(rows, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(rows, "\n")
}

// WordAt is the inclusive column run of non-blank cells around (r, c);
// c0 > c1 when (r, c) is blank.
func (h History) WordAt(r, c int) (c0, c1 int) {
	if x := h.cell(r, c); x.IsZero() && c > 0 {
		c-- // a wide glyph's right half belongs to the glyph
	}
	if blank(h.cell(r, c)) {
		return 1, 0
	}
	c0, c1 = c, c
	for c0 > 0 && !blank(h.cell(r, c0-1)) {
		c0--
	}
	for c1 < h.width-1 && !blank(h.cell(r, c1+1)) {
		c1++
	}
	return c0, c1
}
```

Note `blank` treats a zero cell (`Content == ""`) as blank — a wide right half after a glyph must NOT end a word: in `WordAt`'s two loops, replace `!blank(h.cell(r, x))` with `!h.wordBreak(r, x)`:

```go
// wordBreak: a space, or an empty cell that is not a wide glyph's right half.
func (h History) wordBreak(r, c int) bool {
	x := h.cell(r, c)
	if x.IsZero() && c > 0 && h.cell(r, c-1).Width > 1 {
		return false
	}
	return blank(x)
}
```

and use `h.wordBreak(r, c)` for the initial blank check too. If `uv.Style` names the underline field differently (`grep -n "Underline" $(go env GOMODCACHE)/github.com/charmbracelet/ultraviolet@v0.0.0-20260303162955-0b88c25f3fff/style.go`), use that field/constant.

`internal/domain/sessions.go` aliases:

```go
	SessionHistory  = agentsession.History
	SessionRowMarks = agentsession.RowMarks
```

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/agentsession ./internal/domain -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add internal/agentsession/history.go internal/agentsession/history_test.go internal/domain/sessions.go && git commit -m "feat(agentsession): a frozen History snapshot with copy text and marked rows

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

### Task 6: TUI scroll mode — enter, render, navigate, leave

**Files:**
- Create: `internal/tui/console_scroll.go`
- Modify: `internal/tui/console.go` (`consoleState.scroll`, `renderConsole`, `updateConsoleKey`, `syncConsoleSize`)
- Modify: `internal/tui/console_mouse.go` (normal-screen routing)
- Modify: `internal/tui/footer.go`, `internal/tui/help.go`, `internal/i18n/lang/*.toml`
- Test: `internal/tui/console_scroll_test.go`

**Interfaces:**
- Consumes: `Session.History()`, `History.Len/Row/Taken`, `Session.LastOutput()` (Task 5).
- Produces:
  - `type consoleScroll struct { hist domain.SessionHistory; top, cursor int; sel lineSel; drag charSel }` (`charSel` in Task 8; declare it now as `type charSel struct{ on, active bool; r0, c0, r1, c1 int }`)
  - `consoleState.scroll *consoleScroll`
  - `func (m Model) enterConsoleScroll() Model` — snapshot, view at the bottom, cursor on the last row.
  - `func (m Model) consoleRows() int` — content rows of the current box.
  - `func (m Model) scrollConsoleBy(d int) Model` — moves the view; past the bottom leaves.
  - `func (m Model) consoleScrollKey(msg tea.KeyMsg) (Model, tea.Cmd, bool)` — true = consumed.
  - `func (m Model) leaveConsoleScroll() Model`

- [ ] **Step 1: Write the failing tests**

`internal/tui/console_scroll_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const fortyLines = `i=0; while [ $i -lt 40 ]; do echo "row-$i"; i=$((i+1)); done; printf 'TAIL'; while :; do sleep 5; done`

func TestScrollWheelEntersAndShowsHistory(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	for range 20 {
		m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	}
	if m.console.scroll == nil || m.console.scroll.top != 0 {
		t.Fatalf("scroll = %+v", m.console.scroll)
	}
	if out := m.View(); !strings.Contains(out, "row-0") || strings.Contains(out, "TAIL") {
		t.Fatal("top of history not shown")
	}
}

func TestScrollWheelEntersOnUnfocusedConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m.console.focused, m.focus = false, panelBranches
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	if m.console.scroll == nil || m.focus != panelBranches {
		t.Fatalf("scroll=%v focus=%v", m.console.scroll != nil, m.focus)
	}
}

func TestScrollPastBottomLeaves(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelUp, tea.MouseActionPress)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelDown, tea.MouseActionPress)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelDown, tea.MouseActionPress)
	if m.console.scroll != nil {
		t.Fatal("still scrolling at the bottom")
	}
}

func TestScrollEscNeverReachesAgent(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, `stty raw -echo; printf 'READY\n'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp, Alt: true})
	m = nm.(Model)
	if m.console.scroll == nil {
		t.Fatal("alt+pgup did not enter scroll mode")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	if m.console.scroll != nil || !m.console.focused {
		t.Fatalf("esc: scroll=%v focused=%v", m.console.scroll != nil, m.console.focused)
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m = nm.(Model)
	waitScreen(t, s, "7a")
	if strings.Contains(strings.Join(s.Screen().Lines, ""), "1b") {
		t.Fatal("esc reached the agent")
	}
}

func TestScrollTypedKeyLeavesAndForwards(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, `stty raw -echo; printf 'READY\n'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp, Alt: true})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = nm.(Model)
	if m.console.scroll != nil {
		t.Fatal("typing did not leave scroll mode")
	}
	waitScreen(t, s, "79")
}

func TestScrollKeysMoveCursorAndView(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp, Alt: true})
	m = nm.(Model)
	last := m.console.scroll.hist.Len() - 1
	if m.console.scroll.cursor != last {
		t.Fatalf("cursor %d, want %d", m.console.scroll.cursor, last)
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyHome}} {
		nm, _ = m.Update(k)
		m = nm.(Model)
	}
	if m.console.scroll.cursor != 0 || m.console.scroll.top != 0 {
		t.Fatalf("home: cursor=%d top=%d", m.console.scroll.cursor, m.console.scroll.top)
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm.(Model)
	if m.console.scroll.cursor != 1 {
		t.Fatalf("down: cursor=%d", m.console.scroll.cursor)
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = nm.(Model)
	if m.console.scroll == nil || m.console.scroll.cursor != last {
		t.Fatal("G did not go to the bottom (and must not leave)")
	}
}

func TestScrollResizeLeaves(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp, Alt: true})
	m = nm.(Model)
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = nm.(Model)
	if m.console.scroll != nil {
		t.Fatal("a resize kept the old-width snapshot")
	}
}

func TestScrollTitleShowsPositionAndNewOutput(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, `i=0; while [ $i -lt 40 ]; do echo "row-$i"; i=$((i+1)); done; read _; echo LATER; sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "row-39")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp, Alt: true})
	m = nm.(Model)
	if out := m.View(); !strings.Contains(out, "scroll ↑") || strings.Contains(out, "new output") {
		t.Fatal("title before new output")
	}
	s.SendText("\r")
	waitScreen(t, s, "LATER")
	if out := m.View(); !strings.Contains(out, "new output") {
		t.Fatal("new output not flagged")
	}
}
```

Note `alt+pgup` leaves the ESC-sent check above meaningful: on entry nothing is sent, so the agent's output must never contain `1b`.

- [ ] **Step 2: Run them to see them fail**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui -run TestScroll -count=1`
Expected: build failure — `m.console.scroll undefined`.

- [ ] **Step 3: Implement**

`console.go` — `consoleState` gains `scroll *consoleScroll // scroll mode's frozen view; nil = live`. In `syncConsoleSize`, where the PTY is resized because the size changed, add `m.console.scroll = nil` (the snapshot is the old width). If `syncConsoleSize` returns early when unchanged, put the reset after that check only.

`internal/tui/console_scroll.go`:

```go
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// consoleScroll is scroll mode: a frozen History the user pages through
// while the program runs on underneath (spec §2).
type consoleScroll struct {
	hist   domain.SessionHistory
	top    int     // the first history row shown
	cursor int     // the cursor row (a history index)
	sel    lineSel // space/space/enter whole-line selection
	drag   charSel // a mouse stream selection
}

// charSel is a mouse stream selection over history cells; active while the
// button is held.
type charSel struct {
	on, active     bool
	r0, c0, r1, c1 int
}

// consoleRows is the content rows of the console's box.
func (m Model) consoleRows() int {
	w, h := m.consoleBox()
	_, rows := consoleInner(w, h)
	return rows
}

// enterConsoleScroll freezes the console's history with the view at the
// bottom (exactly the live screen) and the cursor on the last row.
func (m Model) enterConsoleScroll() Model {
	s, ok := m.consoleSession()
	if !ok || m.console.scroll != nil {
		return m
	}
	h := s.History()
	rows := m.consoleRows()
	m.console.scroll = &consoleScroll{hist: h, top: max(h.Len()-rows, 0), cursor: max(h.Len()-1, 0)}
	return m
}

func (m Model) leaveConsoleScroll() Model {
	m.console.scroll = nil
	return m
}

// maxTop is the last view position: the bottom page.
func (sc *consoleScroll) maxTop(rows int) int { return max(sc.hist.Len()-rows, 0) }

// follow keeps the cursor inside the view after the view moved.
func (sc *consoleScroll) follow(rows int) {
	sc.cursor = clampInt(sc.cursor, sc.top, sc.top+rows-1)
	sc.cursor = clampInt(sc.cursor, 0, sc.hist.Len()-1)
}

// reveal moves the view so the cursor shows.
func (sc *consoleScroll) reveal(rows int) {
	if sc.cursor < sc.top {
		sc.top = sc.cursor
	}
	if sc.cursor >= sc.top+rows {
		sc.top = sc.cursor - rows + 1
	}
	sc.top = clampInt(sc.top, 0, sc.maxTop(rows))
}

// scrollConsoleBy moves the view d rows (the wheel); moving down from the
// bottom page leaves scroll mode.
func (m Model) scrollConsoleBy(d int) Model {
	sc := m.console.scroll
	if sc == nil {
		return m
	}
	rows := m.consoleRows()
	if d > 0 && sc.top >= sc.maxTop(rows) && !sc.drag.active {
		return m.leaveConsoleScroll()
	}
	sc.top = clampInt(sc.top+d, 0, sc.maxTop(rows))
	sc.follow(rows)
	return m
}

// consoleScrollKey is scroll mode's keyboard (spec §2). true = consumed;
// false = scroll mode has been left and the key goes on as usual.
func (m Model) consoleScrollKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	sc := m.console.scroll
	rows := m.consoleRows()
	last := sc.hist.Len() - 1
	switch msg.String() {
	case "up", "k":
		sc.cursor = max(sc.cursor-1, 0)
	case "down", "j":
		sc.cursor = min(sc.cursor+1, last)
	case "pgup", "alt+pgup":
		sc.cursor = max(sc.cursor-rows, 0)
		sc.top = max(sc.top-rows, 0)
	case "pgdown", "alt+pgdown":
		if sc.top >= sc.maxTop(rows) {
			return m.leaveConsoleScroll(), nil, true
		}
		sc.cursor = min(sc.cursor+rows, last)
		sc.top = min(sc.top+rows, sc.maxTop(rows))
	case "home", "g":
		sc.cursor, sc.top = 0, 0
	case "end", "G":
		sc.cursor, sc.top = last, sc.maxTop(rows)
	case "esc":
		if sc.sel.on || sc.drag.on {
			sc.sel.clear()
			sc.drag = charSel{}
			return m, nil, true
		}
		return m.leaveConsoleScroll(), nil, true
	case "q":
		return m.leaveConsoleScroll(), nil, true
	default:
		return m.leaveConsoleScroll(), nil, false
	}
	sc.drag = charSel{}
	sc.reveal(rows)
	return m, nil, true
}

// consoleScrollTitle is the title row while scrolling: the position, a
// flag once the program has printed since the snapshot, and the keys.
func (m Model) consoleScrollTitle(s *domain.AgentSession) string {
	sc := m.console.scroll
	t := i18n.T("scroll ↑ %s / %s", fmt.Sprint(sc.top+1), fmt.Sprint(sc.hist.Len()))
	if s.LastOutput().After(sc.hist.Taken()) {
		t += " · " + i18n.T("new output")
	}
	return t + "  " + i18n.T("[esc] leave  [spc] select  [enter] copy")
}

```

`renderConsole` — after `info := sess.Info()`:

```go
		if sc := m.console.scroll; sc != nil {
			lines = append(lines, padRight(truncate(m.consoleScrollTitle(sess), innerW), innerW))
			for i := sc.top; i < sc.hist.Len() && len(lines) < contentH; i++ {
				lines = append(lines, padRight(sc.hist.Row(i, m.consoleRowMarks(i)), innerW))
			}
		} else {
			// … the existing title + screen code, unchanged …
		}
```

and in `console_scroll.go`:

```go
// consoleRowMarks is history row i's highlight: the cursor, the line
// selection (whole row) or the stream selection's span on that row.
func (m Model) consoleRowMarks(i int) domain.SessionRowMarks {
	sc := m.console.scroll
	mk := domain.SessionRowMarks{Cursor: i == sc.cursor}
	if lo, hi, ok := sc.sel.bounds(sc.cursor); ok && i >= lo && i <= hi {
		mk.SelFrom, mk.SelTo = 0, sc.hist.Width()
	}
	return mk
}
```

`updateConsoleKey` — after the focus fix-ups (right before `if m.console.focused {`):

```go
	if m.console.scroll != nil && (m.console.focused || m.focus == panelCommits) {
		var cmd tea.Cmd
		var done bool
		if m, cmd, done = m.consoleScrollKey(msg); done {
			return m, cmd, true
		}
	}
	if m.console.scroll == nil && m.console.focused && key == "alt+pgup" {
		return m.enterConsoleScroll(), nil, true
	}
	if m.console.scroll == nil && !m.console.focused && m.focus == panelCommits && key == "pgup" {
		return m.enterConsoleScroll(), nil, true
	}
```

`console_mouse.go` — replace the final normal-screen `return` in `consoleMouse`:

```go
	m.console.held = tea.MouseButtonNone
	return m.consoleScrollMouse(msg, cx, cy, inContent)
```

and, if the console is scrolling while the program now tracks the mouse or is in the alt screen, leave first — at the top of the `switch` add:

```go
	case m.console.scroll != nil && running && (modes.Mouse || modes.AltScreen) && msg.Action == tea.MouseActionPress:
		m = m.leaveConsoleScroll()
		return m.consoleMouse(msg)
```

with, in `console_scroll.go` (Task 8 extends it with selection):

```go
// consoleScrollMouse is the mouse on a normal-screen console: the wheel
// scrolls (entering scroll mode on the way up).
func (m Model) consoleScrollMouse(msg tea.MouseMsg, cx, cy int, inContent bool) (Model, tea.Cmd, bool) {
	if msg.Action != tea.MouseActionPress {
		return m, nil, true
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m = m.enterConsoleScroll()
		return m.scrollConsoleBy(-m.wheelStep()), nil, true
	case tea.MouseButtonWheelDown:
		return m.scrollConsoleBy(m.wheelStep()), nil, true
	}
	return m, nil, true
}
```

Footer — in `footer.go`, before the existing console lines (line ~266), when `m.console != nil && m.console.scroll != nil`:

```go
		if m.console.scroll != nil {
			return i18n.T("agent console scroll: [↑/↓] move  [pgup/pgdn] page  [g/G] top/bottom  [spc] select  [enter] copy  [drag] copy  [esc/q] leave"), true
		}
```

Help — next to the Task 4 `mouse` row:

```go
		r(i18n.T("wheel / alt+pgup"), i18n.T("over an agent console whose program does not take the mouse (Claude Code's normal mode, Junie, a shell): scroll back through what it printed — the view freezes while it keeps running; ↑/↓ pgup/pgdn g/G move, space…space then enter copies lines, a drag copies text, esc or q leaves, any other key leaves and goes to the agent; pgup on an unfocused console does the same")),
```

i18n: add `"scroll ↑ %s / %s"`, `"new output"`, `"[esc] leave  [spc] select  [enter] copy"`, the footer line, `"wheel / alt+pgup"` and its help text to all four bundles.

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui ./internal/i18n -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add internal/tui/console_scroll.go internal/tui/console_scroll_test.go internal/tui/console.go internal/tui/console_mouse.go internal/tui/footer.go internal/tui/help.go internal/i18n/lang && git commit -m "feat(tui): console scroll mode — a frozen view of the agent's history

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

### Task 7: TUI scroll mode — keyboard line selection and copy

**Files:**
- Modify: `internal/tui/console_scroll.go` (`consoleScrollKey`)
- Test: `internal/tui/console_scroll_test.go`

**Interfaces:**
- Consumes: `lineSel` (`press`, `bounds`, `clear`), `History.Text`, `copyToClipboardCmd`.

- [ ] **Step 1: Write the failing test**

```go
func TestScrollSpaceSpaceEnterCopiesLines(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	keys := []tea.KeyMsg{
		{Type: tea.KeyPgUp, Alt: true}, {Type: tea.KeyHome},
		{Type: tea.KeyDown}, {Type: tea.KeySpace}, {Type: tea.KeyDown}, {Type: tea.KeyDown}, {Type: tea.KeySpace},
	}
	for _, k := range keys {
		nm, _ := m.Update(k)
		m = nm.(Model)
	}
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("enter produced no copy")
	}
	runCmdMsgs(cmd)
	if copied != "row-1\nrow-2\nrow-3" {
		t.Fatalf("copied %q", copied)
	}
	if m.console.scroll == nil || m.console.scroll.sel.on {
		t.Fatal("a copy must stay in scroll mode and clear the selection")
	}
}
```

`runCmdMsgs` — if no helper that runs a (possibly batched) cmd exists in the tui tests (`grep -n "func runCmd\|func drainCmd" internal/tui/*_test.go`), add:

```go
// runCmdMsgs runs cmd and, for a tea.BatchMsg, each of its commands.
func runCmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, runCmdMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}
```

(import `io` in the test file).

- [ ] **Step 2: Run it to see it fail**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui -run TestScrollSpaceSpaceEnter -count=1`
Expected: FAIL — space leaves scroll mode (default branch), `copied` empty.

- [ ] **Step 3: Implement** — in `consoleScrollKey`'s switch, before `case "esc":`:

```go
	case " ":
		sc.sel.press(sc.cursor)
		sc.drag = charSel{}
		return m, nil, true
	case "enter":
		lo, hi, ok := sc.sel.bounds(sc.cursor)
		if !ok {
			lo, hi = sc.cursor, sc.cursor // enter with no selection copies the cursor row
		}
		text := sc.hist.Text(lo, 0, hi, sc.hist.Width()-1)
		sc.sel.clear()
		return m, m.copyToClipboardCmd(i18n.T("Copied %d lines", hi-lo+1), text), true
```

(`msg.String()` for `tea.KeySpace` is `" "`; confirm with `grep -n 'KeySpace' $(go env GOMODCACHE)/github.com/charmbracelet/bubbletea@v1.3.10/key.go` — if it is `"space"`, use that.)

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui -run 'TestScroll' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add internal/tui/console_scroll.go internal/tui/console_scroll_test.go && git commit -m "feat(tui): space/space/enter copies lines in console scroll mode

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

### Task 8: TUI scroll mode — mouse drag, double/triple click, autoscroll

**Files:**
- Modify: `internal/tui/console_scroll.go` (`consoleScrollMouse`, `consoleRowMarks`)
- Modify: `internal/tui/console.go` (`consoleState.clicks`)
- Test: `internal/tui/console_scroll_test.go`

**Interfaces:**
- Consumes: `History.Text`, `History.WordAt`, `History.RowWidth`, `copyToClipboardCmd`.
- Produces: `consoleState.clicks consoleClicks` with `type consoleClicks struct { x, y, n int; at time.Time }`.

- [ ] **Step 1: Write the failing tests**

```go
func dragCopy(t *testing.T, m Model, from, to [2]int) (Model, string) {
	t.Helper()
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	m = mouseAt(m, from[0], from[1], tea.MouseButtonLeft, tea.MouseActionPress)
	m = mouseAt(m, to[0], to[1], tea.MouseButtonLeft, tea.MouseActionMotion)
	nm, cmd := m.Update(tea.MouseMsg{X: to[0], Y: to[1], Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	runCmdMsgs(cmd)
	return nm.(Model), copied
}

func TestScrollDragAtLiveViewCopiesText(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, `printf 'alpha beta\r\ngamma delta'; while :; do sleep 5; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "delta")
	x0, y0 := contentOrigin(m)
	row := func(r int) int { return y0 + r } // the live view: history row Len-rows+r
	m, copied := dragCopy(t, m, [2]int{x0 + 6, row(0)}, [2]int{x0 + 4, row(1)})
	if copied != "beta\ngamma" {
		t.Fatalf("copied %q", copied)
	}
	if m.console.scroll == nil {
		t.Fatal("a drag must enter scroll mode")
	}
}

func TestScrollDoubleClickWordTripleClickLine(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, `printf 'one two.three four'; while :; do sleep 5; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "four")
	x0, y0 := contentOrigin(m)
	click := func() tea.Cmd {
		m = mouseAt(m, x0+6, y0, tea.MouseButtonLeft, tea.MouseActionPress)
		nm, cmd := m.Update(tea.MouseMsg{X: x0 + 6, Y: y0, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
		m = nm.(Model)
		return cmd
	}
	click()
	runCmdMsgs(click())
	if copied != "two.three" {
		t.Fatalf("double click copied %q", copied)
	}
	runCmdMsgs(click())
	if copied != "one two.three four" {
		t.Fatalf("triple click copied %q", copied)
	}
}

func TestScrollDragPastEdgeScrolls(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0, y0+2, tea.MouseButtonLeft, tea.MouseActionPress)
	top := m.console.scroll.top
	m = mouseAt(m, x0, y0-1, tea.MouseButtonLeft, tea.MouseActionMotion) // above the content: the title row
	if m.console.scroll.top != top-1 {
		t.Fatalf("top %d, want %d", m.console.scroll.top, top-1)
	}
	m = mouseAt(m, x0, y0+2, tea.MouseButtonWheelUp, tea.MouseActionPress) // wheel while held extends
	if !m.console.scroll.drag.active || m.console.scroll.top >= top-1 {
		t.Fatal("wheel during a drag did not scroll the held selection")
	}
}
```

The single click in `TestScrollDoubleClickWordTripleClickLine` enters scroll mode and copies nothing (a press+release on one cell is an empty drag — must not copy); the test therefore also guards "a plain click never copies": add `if copied != "" { t.Fatal("a single click copied") }` right after the first `click()`.

- [ ] **Step 2: Run them to see them fail**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui -run 'TestScroll(Drag|Double)' -count=1`
Expected: FAIL — nothing copied (press is ignored by Task 6's `consoleScrollMouse`).

- [ ] **Step 3: Implement**

`console.go` `consoleState`: `clicks consoleClicks // the last press, for double/triple clicks`.

`console_scroll.go` — the type, and the full `consoleScrollMouse` replacing Task 6's:

```go
// consoleClicks counts presses on one cell within clickWindow.
type consoleClicks struct {
	x, y, n int
	at      time.Time
}

const clickWindow = 400 * time.Millisecond

func (c *consoleClicks) press(x, y int, now time.Time) int {
	if c.n > 0 && x == c.x && y == c.y && now.Sub(c.at) <= clickWindow {
		c.n = c.n%3 + 1
	} else {
		c.n = 1
	}
	c.x, c.y, c.at = x, y, now
	return c.n
}

// historyAt maps a content cell to a history row/column in the frozen view,
// clamped to the history.
func (sc *consoleScroll) historyAt(cx, cy, rows int) (r, c int) {
	r = clampInt(sc.top+clampInt(cy, 0, rows-1), 0, sc.hist.Len()-1)
	return r, clampInt(cx, 0, sc.hist.Width()-1)
}

// consoleScrollMouse is the mouse on a normal-screen console (spec §2): the
// wheel scrolls (entering scroll mode on the way up; while a drag is held it
// scrolls and extends it); a press starts a stream selection (entering
// scroll mode frozen at the live view), a second / third press on the same
// cell selects the word / the row; motion extends, past the top or bottom
// edge it scrolls a row; release copies a non-empty selection.
func (m Model) consoleScrollMouse(msg tea.MouseMsg, cx, cy int, inContent bool) (Model, tea.Cmd, bool) {
	rows := m.consoleRows()
	switch {
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelUp:
		m = m.enterConsoleScroll()
		m = m.scrollConsoleBy(-m.wheelStep())
		return m.extendDrag(cx, cy, rows), nil, true
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonWheelDown:
		m = m.scrollConsoleBy(m.wheelStep())
		return m.extendDrag(cx, cy, rows), nil, true
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && inContent:
		m = m.enterConsoleScroll()
		sc := m.console.scroll
		if sc == nil {
			return m, nil, true
		}
		r, c := sc.historyAt(cx, cy, rows)
		sc.sel.clear()
		switch m.console.clicks.press(cx, cy, time.Now()) {
		case 2:
			c0, c1 := sc.hist.WordAt(r, c)
			if c0 > c1 {
				sc.drag = charSel{}
				return m, nil, true
			}
			sc.drag = charSel{on: true, r0: r, c0: c0, r1: r, c1: c1}
			return m, m.copyDrag(), true
		case 3:
			sc.drag = charSel{on: true, r0: r, c0: 0, r1: r, c1: max(sc.hist.RowWidth(r)-1, 0)}
			return m, m.copyDrag(), true
		}
		sc.drag = charSel{on: true, active: true, r0: r, c0: c, r1: r, c1: c}
		sc.cursor = r
		return m, nil, true
	case msg.Action == tea.MouseActionMotion:
		return m.extendDrag(cx, cy, rows), nil, true
	case msg.Action == tea.MouseActionRelease:
		sc := m.console.scroll
		if sc == nil || !sc.drag.active {
			return m, nil, true
		}
		m = m.extendDrag(cx, cy, rows)
		sc.drag.active = false
		if sc.drag.r0 == sc.drag.r1 && sc.drag.c0 == sc.drag.c1 {
			sc.drag = charSel{} // a plain click: nothing selected, nothing copied
			return m, nil, true
		}
		return m, m.copyDrag(), true
	}
	return m, nil, true
}

// extendDrag moves a held selection's far end to the content cell (cx, cy);
// a cell above / below the content scrolls one row first.
func (m Model) extendDrag(cx, cy, rows int) Model {
	sc := m.console.scroll
	if sc == nil || !sc.drag.active {
		return m
	}
	switch {
	case cy < 0:
		sc.top = max(sc.top-1, 0)
	case cy >= rows:
		sc.top = min(sc.top+1, sc.maxTop(rows))
	}
	sc.drag.r1, sc.drag.c1 = sc.historyAt(cx, cy, rows)
	sc.cursor = sc.drag.r1
	return m
}

// copyDrag copies the stream selection.
func (m Model) copyDrag() tea.Cmd {
	d := m.console.scroll.drag
	text := m.console.scroll.hist.Text(d.r0, d.c0, d.r1, d.c1)
	n := d.r1 - d.r0
	if n < 0 {
		n = -n
	}
	return m.copyToClipboardCmd(i18n.T("Copied %d lines", n+1), text)
}
```

`consoleRowMarks` — add the stream selection's span after the line-selection check:

```go
	if d := sc.drag; d.on {
		r0, c0, r1, c1 := d.r0, d.c0, d.r1, d.c1
		if r1 < r0 || (r1 == r0 && c1 < c0) {
			r0, c0, r1, c1 = r1, c1, r0, c0
		}
		if i >= r0 && i <= r1 {
			mk.SelFrom, mk.SelTo = 0, sc.hist.Width()
			if i == r0 {
				mk.SelFrom = c0
			}
			if i == r1 {
				mk.SelTo = c1 + 1
			}
		}
	}
```

Add `"time"` to `console_scroll.go`'s imports. The router (`consoleMouse`) must keep handing a held gg drag to `consoleScrollMouse` when the pointer leaves the box: extend its `held` test to

```go
	held := m.console.held != tea.MouseButtonNone || (m.console.scroll != nil && m.console.scroll.drag.active)
```

- [ ] **Step 4: Run the tests**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go test ./internal/tui -run 'TestScroll|TestConsole' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add internal/tui/console_scroll.go internal/tui/console_scroll_test.go internal/tui/console.go internal/tui/console_mouse.go && git commit -m "feat(tui): drag, double- and triple-click copy in console scroll mode

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

### Task 9: Docs, full gate, manual check

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md` (console section: the router, scroll mode, OSC 52), `CLAUDE.md` (the `agentsession` map row: add "mode tracking, `SendMouse`, OSC 52 capture, frozen `History`" — one line)
- Modify: `docs/superpowers/specs/2026-10-06-console-mouse-scroll-design.md` (record the plan's deviations: no e2e golden screen — the harness has no mouse or session steps, View()-level tests instead; "new output" from `LastOutput` vs the snapshot time; reverse/underline cell marks)

- [ ] **Step 1: Write the docs** — CHANGELOG (Unreleased → Added): "Agent console scroll mode: the wheel (or alt+PgUp, or PgUp on an unfocused console) freezes a view of everything the program printed — Claude Code's normal mode, Junie, shells — while it keeps running; ↑/↓ PgUp/PgDn g/G move, space…space enter copies lines, a drag copies text (double-click a word, triple-click a line), esc/q leaves, any other key leaves and goes to the agent." README: the same in one or two sentences in the agent console section.

- [ ] **Step 2: Run the full gate**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && ./test.sh race 2>&1 | tee /tmp/claude-1000/-work-gigagit/7e6e8d3c-791d-44d0-8979-35caff0d05b3/scratchpad/race.log | tail -5; grep -c "all green" /tmp/claude-1000/-work-gigagit/7e6e8d3c-791d-44d0-8979-35caff0d05b3/scratchpad/race.log`
Expected: the log contains "all green" (count ≥ 1). A `| tail` exit code proves nothing.

- [ ] **Step 3: Build the verify binary and hand over the manual check**

Run: `cd /work/gigagit/.claude/worktrees/console-mouse-scroll && go build -o bin/gg ./cmd/gg && ls -la bin/gg`
Then give the user `/work/gigagit/.claude/worktrees/console-mouse-scroll/bin/gg` and the checks: Claude fullscreen (wheel, drag-copy, paste elsewhere), Claude normal mode (`claude --settings '{"tui":"default"}'` or the user's way) and Junie (wheel scroll back, drag copy, space/space/enter copy, esc leaves without interrupting, typing leaves), mc in Open terminal (clicks, wheel), an unfocused docked console (wheel scrolls, focus stays left).

- [ ] **Step 4: Commit**

```bash
cd /work/gigagit/.claude/worktrees/console-mouse-scroll && git add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md docs/superpowers/specs/2026-10-06-console-mouse-scroll-design.md && git commit -m "docs: console mouse pass-through, scroll mode and copy

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01C62hPr4pGCCKWNg2iR8e16"
```

- [ ] **Step 5: Final review** — one read-only review subagent on the most capable model over `git diff main...feat/console-mouse-scroll` (CLAUDE.md allows review subagents), then fix findings in this session, then ask the user before merging.
