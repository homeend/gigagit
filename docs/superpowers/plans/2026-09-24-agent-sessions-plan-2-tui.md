# Agent Sessions — Plan 2: the TUI console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo NEVER uses subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The user starts an agent from the Worktrees tab, types to it in a live console that takes the Commits panel's place (or the whole body when maximised), steps out with `ctrl+]`, sees running/exited sessions as sub-rows under their worktree, reaches every session from a `ctrl+\` popup, and is asked before quitting gg with live sessions.

**Architecture:** All UI lives in `internal/tui`, over the Plan 1 `domain` surface (`domain.Sessions()`, `SessionCommands`, `EnsureSessionCommands`, `Service.StartSession`, the `Session*` aliases). The console is a right-column replacement like the stash list (`m.console *consoleState`, rendered in `renderInterface`'s right-column switch), with its own key intercept placed directly after the decision modal. Repaints ride a throttled waiter `tea.Cmd` on the session's `Changed()`; list changes ride one on `Sessions().Changed()`. The Worktrees list gains session entries (like the Commits WIP pseudo-rows: `backingIndex` reports `ok=false` for them). Quitting is guarded centrally with `tea.WithFilter` on `tea.QuitMsg`.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, lipgloss, `charmbracelet/ultraviolet` key events (re-exported as `domain.SessionKey`), Plan 1's `internal/agentsession`.

**Spec:** `docs/superpowers/specs/2026-09-24-agent-sessions-design.md` (incl. "Spike findings").

## Global Constraints

- Work only in `/mnt/t/others/gigagit/.claude/worktrees/agent-sessions-tui` (branch `feat/agent-sessions-tui`); `cd` there in EVERY shell command.
- `internal/tui` imports `domain`, never `agentsession` (archtest). Session types come through the `domain` aliases. `ultraviolet` may be imported by `tui` for key constants (it is a UI library, not a gigagit layer).
- Reserved keys, defaults: `ctrl+]` step out one level, `ctrl+\` sessions popup; configurable via `[console] step_out_key` / `sessions_key`. Every other key goes to a focused console — including `esc`, `ctrl+t`, `ctrl+o`, `ctrl+p`, `q`.
- Console state table (spec) — exactly:
  | State | Key | Result |
  |---|---|---|
  | closed | enter on a session sub-row / pick in the popup / Start agent… | docked, focused |
  | docked, focused | ctrl+] | docked, unfocused |
  | docked, unfocused (focus on the Commits slot) | enter | docked, focused |
  | docked, unfocused (focus on the Commits slot) | ctrl+t | maximised + focused |
  | maximised, focused | ctrl+] | docked, unfocused |
  | docked, unfocused (focus on the Commits slot) | esc | closed (session keeps running) |
  | any | ctrl+\ | sessions popup |
- Repaint at most every **33 ms** per session.
- Every user-visible string through `i18n.T` with a literal key present in ja/ko/zh/ru (AST gates).
- New global keys get a `helpContent()` row and a footer binding (`TestHelpFooterCoverage`).
- Windows input (spike findings): drop NUL-only `KeyRunes`; batched `KeyRunes` → `SendText`; a `KeySpace` (normalized from Windows `KeyRunes{' '}`) → `SendText(" ")`.
- TDD, `t.Parallel()` except where global state (`domain.UseSessionManager`) is touched. Tests that start real sessions use `sh` and skip on Windows.
- Commit trailers on every commit:
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01A4Gy5opUFNLUZk6eNCpi43`.

## Review Focus

1. **A focused console must swallow gg's own shortcuts** — `q`, `ctrl+c`, `ctrl+o`, `ctrl+p`, `.`, `?`, `esc`, `ctrl+t` typed into Claude must reach Claude, never quit/open gg surfaces. Pinned in Task 4 (`TestFocusedConsoleForwardsGGShortcuts`).
2. **Worktree actions on a session sub-row** — `d` (delete), `enter` (switch), `e` (move), copy-path must never act on the wrong worktree because a session row shifted the indices. Pinned in Task 5 (`TestSessionRowIsNotAWorktree`, `TestWorktreeActionsIgnoreSessionRows`).
3. **Session outlives its console / its repo** — closing the console, switching worktree, and `R` to another repo keep the session running and listed; opening it from the popup in another repo does not reRoot. Pinned in Task 7 (`TestPopupOpensSessionFromOtherRepoWithoutReRoot`) and Task 3 (`TestReRootKeepsConsole`).
4. **Terminal resize while docked / maximised** — the PTY must follow the box's inner size, or the agent draws for the wrong width. Pinned in Task 3 (`TestConsoleFollowsBoxSize`).
5. **Quit paths other than `q`** — `ctrl+c` inside any popup, the command palette's quit, `gg --at` exits: all ride `tea.QuitMsg` and must hit the guard. Pinned in Task 8 (`TestQuitFilterHoldsQuitWithLiveSessions`).

---

### Task 1: Cursor painting in `Screen`

**Files:**
- Modify: `internal/agentsession/io.go`
- Test: `internal/agentsession/io_test.go`

**Interfaces:**
- Produces: `func (s *Session) Screen() Screen` unchanged, plus `func (s *Session) ScreenWithCursor() Screen` — identical, except that when the cursor is visible, the cursor row is re-rendered from its cells with `uv.AttrReverse` toggled on the cursor cell (an empty cell renders as a reversed space).

- [ ] **Step 1: Failing test**

```go
func TestScreenWithCursorReversesCursorCell(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `printf 'ab'; sleep 2`)
	eventually(t, "text", func() bool { return strings.Contains(s.screenText(), "ab") })
	sc := s.ScreenWithCursor()
	if sc.CursorX != 2 || sc.CursorY != 0 {
		t.Fatalf("cursor = (%d,%d)", sc.CursorX, sc.CursorY)
	}
	if !strings.Contains(sc.Lines[0], "\x1b[7m") {
		t.Fatalf("cursor row has no reverse-video cell: %q", sc.Lines[0])
	}
	if plain := s.Screen(); strings.Contains(plain.Lines[0], "\x1b[7m") {
		t.Fatalf("Screen() must not paint the cursor: %q", plain.Lines[0])
	}
}
```

- [ ] **Step 2: Run — expect FAIL `ScreenWithCursor undefined`**

`cd /mnt/t/others/gigagit/.claude/worktrees/agent-sessions-tui && go test ./internal/agentsession/ -run ScreenWithCursor`

- [ ] **Step 3: Implement** — in `io.go`, split `Screen` into `screen(cursor bool)`; `Screen()` = `screen(false)`, `ScreenWithCursor()` = `screen(true)`. Inside `screen`, after computing `lines` and `pos` (still under `ioMu`):

```go
	if cursor && !s.cursorHidden.Load() && pos.Y >= 0 && pos.Y < h && pos.X >= 0 && pos.X < w {
		row := make(uv.Line, w)
		for x := 0; x < w; x++ {
			if c := s.emu.CellAt(x, pos.Y); c != nil {
				row[x] = *c
			} else {
				row[x] = uv.EmptyCell
			}
		}
		cc := row[pos.X]
		if cc.IsZero() || cc.Content == "" {
			cc = uv.EmptyCell
		}
		cc.Style.Attrs ^= uv.AttrReverse
		row[pos.X] = cc
		lines[pos.Y] = row.Render()
	}
```

(`uv.EmptyCell` is a space with no style; toggling reverse on it makes it non-empty so `Render` emits it.) If the pinned `ultraviolet` names the field differently, adapt and note it in the ledger.

- [ ] **Step 4: Run — expect PASS**, then `go test -race -count=1 ./internal/agentsession/`.

- [ ] **Step 5: Commit** `feat(agentsession): ScreenWithCursor paints the cursor cell reversed`.

---

### Task 2: Key encoding (tea → session input)

**Files:**
- Create: `internal/tui/console_keys.go`
- Test: `internal/tui/console_keys_test.go`

**Interfaces:**
- Produces:
  ```go
  // consoleInput is what one tea.KeyMsg sends to a session: exactly one of
  // text, paste, key is set; drop = send nothing.
  type consoleInput struct {
      text  string
      paste string
      key   domain.SessionKey
      drop  bool
  }
  func encodeConsoleKey(k tea.KeyMsg) consoleInput
  ```

- [ ] **Step 1: Failing table test**

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

func TestEncodeConsoleKey(t *testing.T) {
	t.Parallel()
	press := func(code rune, mod uv.KeyMod) uv.KeyPressEvent { return uv.KeyPressEvent{Code: code, Mod: mod} }
	cases := []struct {
		name string
		in   tea.KeyMsg
		text string
		pst  string
		key  any
		drop bool
	}{
		{"batched runes → text", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("héllo")}, "héllo", "", nil, false},
		{"windows bare modifier → drop", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}}, "", "", nil, true},
		{"space → text", tea.KeyMsg{Type: tea.KeySpace}, " ", "", nil, false},
		{"paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb"), Paste: true}, "", "a\nb", nil, false},
		{"alt+b", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b"), Alt: true}, "", "", uv.KeyPressEvent{Code: 'b', Text: "b", Mod: uv.ModAlt}, false},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "", "", press(uv.KeyEnter, 0), false},
		{"alt+enter", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, "", "", press(uv.KeyEnter, uv.ModAlt), false},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "", "", press(uv.KeyEscape, 0), false},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "", "", press(uv.KeyTab, 0), false},
		{"shift+tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "", "", press(uv.KeyTab, uv.ModShift), false},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "", "", press(uv.KeyBackspace, 0), false},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "", "", press(uv.KeyUp, 0), false},
		{"ctrl+left", tea.KeyMsg{Type: tea.KeyCtrlLeft}, "", "", press(uv.KeyLeft, uv.ModCtrl), false},
		{"shift+up", tea.KeyMsg{Type: tea.KeyShiftUp}, "", "", press(uv.KeyUp, uv.ModShift), false},
		{"pgdown", tea.KeyMsg{Type: tea.KeyPgDown}, "", "", press(uv.KeyPgDown, 0), false},
		{"delete", tea.KeyMsg{Type: tea.KeyDelete}, "", "", press(uv.KeyDelete, 0), false},
		{"f5", tea.KeyMsg{Type: tea.KeyF5}, "", "", press(uv.KeyF5, 0), false},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}, "", "", press('c', uv.ModCtrl), false},
		{"ctrl+o", tea.KeyMsg{Type: tea.KeyCtrlO}, "", "", press('o', uv.ModCtrl), false},
		{"ctrl+underscore", tea.KeyMsg{Type: tea.KeyCtrlUnderscore}, "", "", press('_', uv.ModCtrl), false},
	}
	for _, c := range cases {
		got := encodeConsoleKey(c.in)
		if got.text != c.text || got.paste != c.pst || got.drop != c.drop {
			t.Errorf("%s: got %+v", c.name, got)
			continue
		}
		if c.key == nil {
			if got.key != nil {
				t.Errorf("%s: unexpected key %v", c.name, got.key)
			}
			continue
		}
		if got.key != c.key {
			t.Errorf("%s: key = %#v, want %#v", c.name, got.key, c.key)
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL** (`encodeConsoleKey undefined`).

`go test ./internal/tui/ -run TestEncodeConsoleKey`

- [ ] **Step 3: Implement `console_keys.go`**

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/homeend/gigagit/internal/domain"
)

// consoleInput is what one tea.KeyMsg sends to a session: exactly one of
// text, paste, key is set; drop = send nothing.
type consoleInput struct {
	text  string
	paste string
	key   domain.SessionKey
	drop  bool
}

// consoleSpecial maps Bubble Tea's non-rune key types to the key code and
// modifiers the session's emulator encodes (it honours the child's terminal
// modes, e.g. application cursor keys).
var consoleSpecial = map[tea.KeyType]uv.KeyPressEvent{
	tea.KeyEnter: {Code: uv.KeyEnter}, tea.KeyTab: {Code: uv.KeyTab},
	tea.KeyShiftTab: {Code: uv.KeyTab, Mod: uv.ModShift},
	tea.KeyEsc: {Code: uv.KeyEscape}, tea.KeyBackspace: {Code: uv.KeyBackspace},
	tea.KeyDelete: {Code: uv.KeyDelete}, tea.KeyInsert: {Code: uv.KeyInsert},
	tea.KeyUp: {Code: uv.KeyUp}, tea.KeyDown: {Code: uv.KeyDown},
	tea.KeyLeft: {Code: uv.KeyLeft}, tea.KeyRight: {Code: uv.KeyRight},
	tea.KeyHome: {Code: uv.KeyHome}, tea.KeyEnd: {Code: uv.KeyEnd},
	tea.KeyPgUp: {Code: uv.KeyPgUp}, tea.KeyPgDown: {Code: uv.KeyPgDown},
	tea.KeyShiftUp: {Code: uv.KeyUp, Mod: uv.ModShift}, tea.KeyShiftDown: {Code: uv.KeyDown, Mod: uv.ModShift},
	tea.KeyShiftLeft: {Code: uv.KeyLeft, Mod: uv.ModShift}, tea.KeyShiftRight: {Code: uv.KeyRight, Mod: uv.ModShift},
	tea.KeyShiftHome: {Code: uv.KeyHome, Mod: uv.ModShift}, tea.KeyShiftEnd: {Code: uv.KeyEnd, Mod: uv.ModShift},
	tea.KeyCtrlUp: {Code: uv.KeyUp, Mod: uv.ModCtrl}, tea.KeyCtrlDown: {Code: uv.KeyDown, Mod: uv.ModCtrl},
	tea.KeyCtrlLeft: {Code: uv.KeyLeft, Mod: uv.ModCtrl}, tea.KeyCtrlRight: {Code: uv.KeyRight, Mod: uv.ModCtrl},
	tea.KeyCtrlHome: {Code: uv.KeyHome, Mod: uv.ModCtrl}, tea.KeyCtrlEnd: {Code: uv.KeyEnd, Mod: uv.ModCtrl},
	tea.KeyCtrlPgUp: {Code: uv.KeyPgUp, Mod: uv.ModCtrl}, tea.KeyCtrlPgDown: {Code: uv.KeyPgDown, Mod: uv.ModCtrl},
	tea.KeyCtrlShiftUp: {Code: uv.KeyUp, Mod: uv.ModCtrl | uv.ModShift}, tea.KeyCtrlShiftDown: {Code: uv.KeyDown, Mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlShiftLeft: {Code: uv.KeyLeft, Mod: uv.ModCtrl | uv.ModShift}, tea.KeyCtrlShiftRight: {Code: uv.KeyRight, Mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlAt: {Code: '@', Mod: uv.ModCtrl}, tea.KeyCtrlBackslash: {Code: '\\', Mod: uv.ModCtrl},
	tea.KeyCtrlCloseBracket: {Code: ']', Mod: uv.ModCtrl}, tea.KeyCtrlCaret: {Code: '^', Mod: uv.ModCtrl},
	tea.KeyCtrlUnderscore: {Code: '_', Mod: uv.ModCtrl}, tea.KeyCtrlQuestionMark: {Code: '?', Mod: uv.ModCtrl},
	tea.KeyF1: {Code: uv.KeyF1}, tea.KeyF2: {Code: uv.KeyF2}, tea.KeyF3: {Code: uv.KeyF3}, tea.KeyF4: {Code: uv.KeyF4},
	tea.KeyF5: {Code: uv.KeyF5}, tea.KeyF6: {Code: uv.KeyF6}, tea.KeyF7: {Code: uv.KeyF7}, tea.KeyF8: {Code: uv.KeyF8},
	tea.KeyF9: {Code: uv.KeyF9}, tea.KeyF10: {Code: uv.KeyF10}, tea.KeyF11: {Code: uv.KeyF11}, tea.KeyF12: {Code: uv.KeyF12},
}

// encodeConsoleKey turns one Bubble Tea key into session input.
func encodeConsoleKey(k tea.KeyMsg) consoleInput {
	if k.Paste {
		return consoleInput{paste: string(k.Runes)}
	}
	switch k.Type {
	case tea.KeyRunes:
		s := string(k.Runes)
		if strings.Trim(s, "\x00") == "" {
			// Windows: a bare Ctrl/Alt/Win key-down arrives as KeyRunes{0}
			// (Bubble Tea's console reader filters only Shift).
			return consoleInput{drop: true}
		}
		if k.Alt && len(k.Runes) == 1 {
			return consoleInput{key: uv.KeyPressEvent{Code: k.Runes[0], Text: s, Mod: uv.ModAlt}}
		}
		return consoleInput{text: s} // batched fast typing arrives as one message
	case tea.KeySpace:
		return consoleInput{text: " "}
	}
	if ev, ok := consoleSpecial[k.Type]; ok {
		if k.Alt {
			ev.Mod |= uv.ModAlt
		}
		return consoleInput{key: ev}
	}
	if k.Type >= tea.KeyCtrlA && k.Type <= tea.KeyCtrlZ {
		ev := uv.KeyPressEvent{Code: rune('a' + int(k.Type-tea.KeyCtrlA)), Mod: uv.ModCtrl}
		if k.Alt {
			ev.Mod |= uv.ModAlt
		}
		return consoleInput{key: ev}
	}
	return consoleInput{drop: true}
}
```

Note: in Bubble Tea v1 `KeyEnter == KeyCtrlM`, `KeyTab == KeyCtrlI`, `KeyEsc == KeyCtrlOpenBracket`, `KeyBackspace` is DEL (127) while `KeyCtrlH` is BS (8) — the map entries for Enter/Tab/Esc are checked before the ctrl-letter range, so those keys keep their names. Verify with the test; if a collision makes a map literal duplicate-key compile error, drop the alias entry and add a comment.

- [ ] **Step 4: Run — expect PASS**.
- [ ] **Step 5: Commit** `feat(tui): encode Bubble Tea keys as agent-session input`.

---

### Task 3: Console state, rendering, size sync, repaint waiter

**Files:**
- Create: `internal/tui/console.go`
- Modify: `internal/tui/model.go` (Model field `console *consoleState`; `WindowSizeMsg` arm calls `m.syncConsoleSize()`; new msg arms), `internal/tui/view.go` (right-column switch + maximised body)
- Test: `internal/tui/console_test.go`

**Interfaces:**
- Consumes: `domain.Sessions()`, `domain.AgentSession` (`Info`, `ScreenWithCursor`, `Screen`, `Resize`, `Changed`, `Done`), `domain.SessionID`.
- Produces:
  ```go
  type consoleState struct {
      id        domain.SessionID
      focused   bool // keys go to the agent
      maximized bool
      gen       int  // bumps on open/close: drops stale repaint msgs
  }
  type consoleChangedMsg struct{ id domain.SessionID; gen int }
  type sessionsChangedMsg struct{}
  func (m Model) openConsole(id domain.SessionID) (Model, tea.Cmd) // docked + focused, focus = panelCommits
  func (m Model) closeConsole() Model                             // back to Commits, focus restored like closeStashView
  func (m Model) consoleSession() (*domain.AgentSession, bool)
  func (m Model) consoleBox() (w, h int)                          // the box the console draws in right now
  func (m Model) syncConsoleSize() Model                          // Resize the PTY to the box's inner size
  func (m Model) renderConsole(boxW, boxH int) string
  func waitSessionCmd(s *domain.AgentSession, id domain.SessionID, gen int) tea.Cmd
  func waitSessionsCmd() tea.Cmd
  func consoleInner(boxW, boxH int) (cols, rows int) // boxW-4, boxH-3 (border 2 + title 1)
  ```

- [ ] **Step 1: Failing tests** (`console_test.go`)

```go
package tui

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// startTestSession starts `sh -c script` through the process-global manager
// the TUI reads. Serial tests only (UseSessionManager is global).
func startTestSession(t *testing.T, m Model, script string) *domain.AgentSession {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() {
		domain.Sessions().KillAll(t.Context())
		restore()
	})
	dir := m.currentWorktree
	if dir == "" {
		dir = t.TempDir()
	}
	s, err := m.svc.StartSession(t.Context(), config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: script}, dir, 80, 20)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func waitScreen(t *testing.T, s *domain.AgentSession, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range s.Screen().Lines {
			if strings.Contains(l, want) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q", want)
}

func TestOpenConsoleDocksFocused(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `printf 'HELLO-CONSOLE'; sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	if m.console == nil || !m.console.focused || m.console.maximized || m.focus != panelCommits {
		t.Fatalf("console = %+v focus=%v", m.console, m.focus)
	}
	waitScreen(t, s, "HELLO-CONSOLE")
	if out := m.View(); !strings.Contains(out, "HELLO-CONSOLE") {
		t.Fatal("docked console output missing from the frame")
	}
}

func TestConsoleFollowsBoxSize(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if sc := s.Screen(); sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("docked emulator %dx%d, want %dx%d", sc.Cols, sc.Rows, cols, rows)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	m = mm.(Model)
	w, h = m.consoleBox()
	cols, rows = consoleInner(w, h)
	if sc := s.Screen(); sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("after resize emulator %dx%d, want %dx%d", sc.Cols, sc.Rows, cols, rows)
	}
	m.console.maximized = true
	m = m.syncConsoleSize()
	w, h = m.consoleBox()
	if w != m.layout().w {
		t.Fatalf("maximised box width %d, want full %d", w, m.layout().w)
	}
}

func TestCloseConsoleRestoresCommits(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	m = m.rememberLeftFocus()
	m, _ = m.openConsole(s.Info().ID)
	m = m.closeConsole()
	if m.console != nil || m.focus != panelWorktrees {
		t.Fatalf("console=%v focus=%v", m.console, m.focus)
	}
	if s.Info().State != domain.SessionRunning {
		t.Fatal("closing the console must not end the session")
	}
}

func TestReRootKeepsConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	mm, _ := m.reRoot(m.currentWorktree)
	m = mm.(Model)
	if m.console == nil || m.console.id != s.Info().ID {
		t.Fatal("a worktree/repo switch must not close the console")
	}
}

func TestStaleConsoleRepaintDropped(t *testing.T) {
	m := loadedModel(t)
	m.console = &consoleState{id: "s9", gen: 2}
	mm, cmd := m.Update(consoleChangedMsg{id: "s9", gen: 1})
	if cmd != nil {
		t.Fatal("a stale generation must not re-arm the waiter")
	}
	_ = mm
}
```

(Add `tea "github.com/charmbracelet/bubbletea"` to the imports.)

- [ ] **Step 2: Run — expect FAIL** (`m.openConsole undefined`).

`go test ./internal/tui/ -run 'Console'`

- [ ] **Step 3: Implement `console.go`**

```go
package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// consoleRepaint is the minimum spacing of console repaints: a chatty agent
// must not turn every output chunk into a full frame.
const consoleRepaint = 33 * time.Millisecond

// consoleState is the agent console shown in the Commits column (docked) or
// over the whole body (maximised). The session itself lives in
// domain.Sessions(); closing the console never ends it.
type consoleState struct {
	id        domain.SessionID
	focused   bool
	maximized bool
	gen       int
}

type consoleChangedMsg struct {
	id  domain.SessionID
	gen int
}

type sessionsChangedMsg struct{}

func (m Model) consoleSession() (*domain.AgentSession, bool) {
	if m.console == nil {
		return nil, false
	}
	return domain.Sessions().Get(m.console.id)
}

// openConsole shows session id docked in the Commits column with keyboard
// focus. The left panel that had focus is remembered for closeConsole.
func (m Model) openConsole(id domain.SessionID) (Model, tea.Cmd) {
	s, ok := domain.Sessions().Get(id)
	if !ok {
		m.statusMsg = i18n.T("that agent session is gone")
		return m, nil
	}
	gen := 1
	if m.console != nil {
		gen = m.console.gen + 1
	}
	if m.focus != panelCommits {
		m = m.rememberLeftFocus()
	}
	m.stashView = nil
	m.filesPreview = nil
	m.console = &consoleState{id: id, focused: true, gen: gen}
	m.focus = panelCommits
	m = m.syncConsoleSize()
	return m, waitSessionCmd(s, id, gen)
}

// closeConsole hides the console; the session keeps running.
func (m Model) closeConsole() Model {
	m.console = nil
	m.focus = m.lastLeftPanel
	return m.reconcileFullscreenFocus()
}

// consoleBox is the box the console occupies now: the Commits column, or the
// whole body when maximised.
func (m Model) consoleBox() (w, h int) {
	g := m.layout()
	if m.console != nil && m.console.maximized {
		return g.w, g.bodyH
	}
	if g.w < 40 {
		return g.w, g.boxH[panelCommits]
	}
	return g.rightW, g.boxH[panelCommits]
}

// consoleInner is the emulator size for a box: border (2) + padding (2)
// across, border (2) + the title line down.
func consoleInner(boxW, boxH int) (cols, rows int) {
	return max(boxW-4, 1), max(boxH-3, 1)
}

// syncConsoleSize resizes the shown session's PTY to its box (a no-op when
// unchanged, so it is safe to call after every layout-affecting event).
func (m Model) syncConsoleSize() Model {
	s, ok := m.consoleSession()
	if !ok {
		return m
	}
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	_ = s.Resize(cols, rows)
	return m
}

// waitSessionCmd blocks until session s changes, then waits out the repaint
// spacing (absorbing further changes) before asking for a frame.
func waitSessionCmd(s *domain.AgentSession, id domain.SessionID, gen int) tea.Cmd {
	return func() tea.Msg {
		<-s.Changed()
		time.Sleep(consoleRepaint)
		return consoleChangedMsg{id: id, gen: gen}
	}
}

// waitSessionsCmd reports list-level changes (start, exit, remove).
func waitSessionsCmd() tea.Cmd {
	ch := domain.Sessions().Changed()
	return func() tea.Msg {
		<-ch
		return sessionsChangedMsg{}
	}
}

// consoleTitle is the box title: label · worktree · state.
func consoleTitle(info domain.SessionInfo, focused bool) string {
	state := i18n.T("running %s", formatElapsed(time.Since(info.Started)))
	if info.State == domain.SessionExited {
		state = i18n.T("exited (%d)", info.ExitCode)
	}
	t := info.Label + " · " + shortWorktreeName(info.Dir) + " · " + state
	if !focused {
		t += "  " + i18n.T("[enter] type  [ctrl+t] maximise  [esc] close")
	}
	return t
}

// renderConsole draws the console box. The emulator lines are ANSI strings
// sized to the inner width; Render drops trailing blanks, so each is padded.
func (m Model) renderConsole(boxW, boxH int) string {
	s := st()
	contentH := max(boxH-2, 1)
	innerW := max(boxW-4, 1)
	sess, ok := m.consoleSession()
	var lines []string
	if !ok {
		lines = []string{padRight(i18n.T("(agent session gone)"), innerW)}
	} else {
		info := sess.Info()
		lines = append(lines, padRight(truncate(consoleTitle(info, m.console.focused), innerW), innerW))
		var sc domain.SessionScreen
		if m.console.focused && info.State == domain.SessionRunning {
			sc = sess.ScreenWithCursor()
		} else {
			sc = sess.Screen()
		}
		for _, l := range sc.Lines {
			if len(lines) >= contentH {
				break
			}
			lines = append(lines, padRight(l, innerW))
		}
	}
	for len(lines) < contentH {
		lines = append(lines, padRight("", innerW))
	}
	style := s.bluredPanel
	if m.focus == panelCommits {
		style = s.focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
}
```

`shortWorktreeName(path string) string` — `filepath.Base(path)`; add it here if no equivalent helper exists (grep `func .*Base(` in `internal/tui` first and reuse one).

Model wiring (`model.go`):
- Field next to `stashView`: `console *consoleState // agent console over the Commits column (or maximised); nil = closed`.
- In `dispatch`, `case tea.WindowSizeMsg:` after `m.height = msg.Height`: `m = m.syncConsoleSize()`.
- New arms in `dispatch`:

```go
	case consoleChangedMsg:
		if m.console == nil || msg.id != m.console.id || msg.gen != m.console.gen {
			return m, nil // a closed or replaced console's waiter dies here
		}
		s, ok := m.consoleSession()
		if !ok {
			return m, nil
		}
		return m, waitSessionCmd(s, msg.id, msg.gen)
	case sessionsChangedMsg:
		return m.onSessionsChanged()
```

`onSessionsChanged` (in `console.go`) returns `m, waitSessionsCmd()` for now; Task 8 adds exit notices. Arm `waitSessionsCmd()` once from `Init()` (append to its batch).

View wiring (`view.go` `renderInterface`):
- Before `var left string`, handle maximised:

```go
	if m.console != nil && m.console.maximized {
		body := m.renderConsole(g.w, g.bodyH)
		return strings.Join([]string{header, body, footer, statusRow}, "\n")
	}
```

- In the right-column `switch`, first case after `g.boxH[panelCommits] <= 0`: `case m.console != nil: right = m.renderConsole(g.rightW, g.boxH[panelCommits])`.
- In the `g.w < 40` narrow branch: if `m.console != nil`, render the console instead of commits.

Also: `inContentWindow()` / `m.filesView != nil || m.stashView != nil || m.filesPreview != nil` (model.go ~4041) — add `|| m.console != nil` ONLY if that predicate means "the right column is replaced"; read its doc comment first and ledger the decision. In `reRoot`, do NOT clear `m.console` (TestReRootKeepsConsole) but DO bump nothing — the waiter stays valid. `openStashView` / file preview openers: close the console first (`m.console = nil`) so two right-column owners never coexist; grep their openers and add it.

- [ ] **Step 4: Run — expect PASS** `go test ./internal/tui/ -run 'Console'`, then `go vet ./internal/tui/`.
- [ ] **Step 5: Commit** `feat(tui): agent console in the Commits column with size sync and throttled repaint`.

---

### Task 4: Key routing and the state machine

**Files:**
- Modify: `internal/tui/model.go` (intercept after the modal block), `internal/tui/console.go`
- Test: `internal/tui/console_keys_route_test.go`

**Interfaces:**
- Produces: `func (m Model) updateConsoleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool)` (handled flag), `func (m Model) stepOutKey() string` / `func (m Model) sessionsKey() string` (config-backed, defaults `ctrl+]` / `ctrl+\`; Task 9 adds config — until then return the defaults), `func (m Model) openSessionsPopup(quitMode bool) (Model, tea.Cmd)` (stub until Task 7: push nothing, set `m.statusMsg`).

- [ ] **Step 1: Failing tests**

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func ctrlBracket() tea.KeyMsg   { return tea.KeyMsg{Type: tea.KeyCtrlCloseBracket} }
func ctrlBackslash() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlBackslash} }

func TestConsoleStateMachine(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	step := func(k tea.KeyMsg) {
		t.Helper()
		mm, _ := m.Update(k)
		m = mm.(Model)
	}
	step(ctrlBracket()) // docked focused → docked unfocused
	if m.console == nil || m.console.focused || m.console.maximized || m.focus != panelCommits {
		t.Fatalf("after ctrl+]: %+v focus=%v", m.console, m.focus)
	}
	step(keyMsg("enter")) // → focused
	if !m.console.focused {
		t.Fatal("enter on the unfocused console must focus it")
	}
	step(ctrlBracket())
	step(keyMsg("ctrl+t")) // → maximised + focused
	if !m.console.maximized || !m.console.focused {
		t.Fatalf("ctrl+t: %+v", m.console)
	}
	step(ctrlBracket()) // maximised focused → docked unfocused
	if m.console.maximized || m.console.focused {
		t.Fatalf("ctrl+] from maximised: %+v", m.console)
	}
	step(keyMsg("esc")) // → closed
	if m.console != nil {
		t.Fatal("esc on the unfocused console must close it")
	}
	if s.Info().State != 0 { // domain.SessionRunning
		t.Fatal("the session must keep running")
	}
}

func TestFocusedConsoleForwardsGGShortcuts(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	// The child echoes what it reads in raw mode so we can see every byte.
	s := startTestSession(t, m, `stty raw -echo; while :; do dd bs=1 count=1 2>/dev/null | od -An -c; done`)
	m, _ = m.openConsole(s.Info().ID)
	for _, k := range []tea.KeyMsg{keyMsg("q"), {Type: tea.KeyCtrlC}, {Type: tea.KeyCtrlO}, {Type: tea.KeyCtrlP}, keyMsg("."), keyMsg("?"), keyMsg("esc"), keyMsg("ctrl+t")} {
		mm, cmd := m.Update(k)
		m = mm.(Model)
		if m.console == nil || !m.console.focused || m.console.maximized {
			t.Fatalf("%q changed console state: %+v", k.String(), m.console)
		}
		if m.topLayer() != nil || m.actionMenu != nil {
			t.Fatalf("%q opened a gg surface", k.String())
		}
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("%q quit gg", k.String())
			}
		}
	}
	waitScreen(t, s, "003") // ctrl+c arrived as ETX
	waitScreen(t, s, "017") // ctrl+o arrived as SI
}

func TestCtrlBackslashFromPanels(t *testing.T) {
	m := loadedModel(t)
	mm, _ := m.Update(ctrlBackslash())
	m = mm.(Model)
	if _, ok := m.topLayer().(*sessionsPopup); !ok {
		t.Fatalf("ctrl+\\ must open the sessions popup, top = %T", m.topLayer())
	}
}
```

(`TestCtrlBackslashFromPanels` fails until Task 7 introduces `sessionsPopup`; mark it `t.Skip("Task 7")` now and remove the skip in Task 7 — ledger it.)

- [ ] **Step 2: Run — expect FAIL** (`updateConsoleKey undefined` / state assertions).

- [ ] **Step 3: Implement**

In `model.go`, directly after the `if m.modal != nil { … }` block and BEFORE the `ctrl+o` block:

```go
		// The agent console: a FOCUSED console owns every key except the two
		// reserved ones (spec), ahead of ctrl+o / ctrl+p / the layer stack —
		// agents use those chords themselves. An unfocused console only
		// claims enter / ctrl+t / esc while its column has focus.
		if nm, cmd, handled := m.updateConsoleKey(msg); handled {
			return nm, cmd
		}
```

In `console.go`:

```go
// updateConsoleKey routes a key to/around the console per the state table.
func (m Model) updateConsoleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	key := msg.String()
	if key == m.sessionsKey() && m.proc == nil {
		nm, cmd := m.openSessionsPopup(false)
		return nm, cmd, true
	}
	if m.console == nil {
		return m, nil, false
	}
	if m.console.focused {
		if key == m.stepOutKey() {
			m.console.focused = false
			if m.console.maximized {
				m.console.maximized = false
				m = m.syncConsoleSize()
			}
			return m, nil, true
		}
		if s, ok := m.consoleSession(); ok && s.Info().State == domain.SessionRunning {
			in := encodeConsoleKey(msg)
			switch {
			case in.drop:
			case in.paste != "":
				s.Paste(in.paste)
			case in.text != "":
				s.SendText(in.text)
			case in.key != nil:
				s.SendKey(in.key)
			}
		}
		return m, nil, true
	}
	// Unfocused: only while the console's column has focus and nothing is
	// layered above it.
	if m.focus != panelCommits || m.topLayer() != nil || m.actionMenu != nil {
		return m, nil, false
	}
	switch key {
	case "enter":
		m.console.focused = true
		return m, nil, true
	case "ctrl+t":
		m.console.maximized, m.console.focused = true, true
		return m.syncConsoleSize(), nil, true
	case "esc":
		return m.closeConsole(), nil, true
	}
	return m, nil, false
}
```

Keys to an EXITED focused console are swallowed (no-op) — the title shows `exited (N)`; `ctrl+]` still steps out. The docked-unfocused console must also swallow j/k/space etc. that would otherwise move the (hidden) Commits cursor: after the switch, `return m, nil, true` for any key that the Commits panel would handle — simplest: when unfocused and `m.focus == panelCommits`, let `tab`/`shift+tab`/`left`/`h`/`?`/`.`/`q`/`ctrl+c`/`ctrl+p`/`ctrl+o` fall through (return false) and swallow everything else. Add a test row for `j` not moving `m.sel[panelCommits]`.

`stepOutKey`/`sessionsKey` return `"ctrl+]"` / `"ctrl+\\"` (Task 9 makes them configurable). `openSessionsPopup` stub: `m.statusMsg = i18n.T("no agent sessions"); return m, nil`.

- [ ] **Step 4: Run — expect PASS** (the popup test skipped).
- [ ] **Step 5: Commit** `feat(tui): console key routing — ctrl+] steps out, enter/ctrl+t/esc on the docked console`.

---

### Task 5: Session sub-rows in the Worktrees list

**Files:**
- Modify: `internal/tui/viewstate.go` (`worktreeList`, `listFor`, `backingIndex`), `internal/tui/view.go` (`worktreeRows`), `internal/tui/selection.go:40-56`, `internal/tui/find_current.go`, `internal/tui/avail.go` (`selectedWorktree`), `internal/tui/action_menu.go:696-703`, `internal/tui/model.go` (enter on a session row)
- Create: `internal/tui/worktree_sessions.go`
- Test: `internal/tui/worktree_sessions_test.go`

**Interfaces:**
- Produces:
  ```go
  // wtEntry is one Worktrees list row: a worktree (sess == "") or one of
  // its agent sessions.
  type wtEntry struct {
      wt   int              // index into m.worktrees
      sess domain.SessionID // "" = the worktree row itself
  }
  func (m Model) worktreeEntries() []wtEntry          // worktree, then its sessions (oldest first)
  func (m Model) selectedSession() (domain.SessionInfo, bool) // ok only on a session sub-row
  func sessionRowText(info domain.SessionInfo) string // "  └ ● Claude  running 12m" / "  └ ○ Codex  exited (0)"
  ```
- `backingIndex(panelWorktrees)` keeps returning an index into `m.worktrees` (translated through the entry), and `ok=false` on a session row — so every existing caller (`selectedWorktree`, copy rows, delete/move/switch gates) ignores session rows for free.

- [ ] **Step 1: Failing tests**

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestSessionSubRowUnderItsWorktree(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 5`)
	rows, _ := m.panelView(panelWorktrees)
	var at int = -1
	for i, r := range rows {
		if strings.Contains(r, "└") && strings.Contains(r, "Shell") {
			at = i
		}
	}
	if at < 1 {
		t.Fatalf("no session sub-row under the worktree: %q", rows)
	}
	if !strings.Contains(rows[at-1], m.currentWorktree) {
		t.Fatalf("sub-row not directly under its worktree: %q", rows)
	}
	_ = s
}

func TestSessionRowIsNotAWorktree(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1 // the session row under the only worktree
	if _, ok := m.selectedWorktree(); ok {
		t.Fatal("a session row must not resolve to a worktree")
	}
	if info, ok := m.selectedSession(); !ok || info.Label != "Shell" {
		t.Fatalf("selectedSession = %+v, %v", info, ok)
	}
}

func TestWorktreeActionsIgnoreSessionRows(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	if m.canDeleteWorktree() || m.canEnterWorktree() || m.canMoveWorktree() {
		t.Fatal("worktree actions must be off on a session row")
	}
	for _, r := range availableActions(m) {
		if r.id == "copy-worktree-abspath" {
			t.Fatal("copy worktree path offered on a session row")
		}
	}
}

func TestEnterOnSessionRowOpensConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.console == nil || m.console.id != s.Info().ID || !m.console.focused {
		t.Fatalf("enter on a session row: console = %+v", m.console)
	}
}

func TestSessionRowsFollowSortAndFilter(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`)
	m.sortModes[panelWorktrees] = sortNameDesc
	rows, _ := m.panelView(panelWorktrees)
	if len(rows) != 2 || !strings.Contains(rows[1], "└") {
		t.Fatalf("sorted rows = %q", rows)
	}
	_ = domain.SessionRunning
}
```

(`loadedModel`'s repo has one worktree; if `newRepo` produces more, adjust indices by locating the worktree row with `m.currentWorktree` — ledger it.)

- [ ] **Step 2: Run — expect FAIL**.

- [ ] **Step 3: Implement**

`worktree_sessions.go`:

```go
package tui

import (
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

type wtEntry struct {
	wt   int
	sess domain.SessionID
}

// worktreeEntries is the Worktrees list in display order: each worktree
// followed by its agent sessions (this repo's only — other repos' sessions
// live in the ctrl+\ popup).
func (m Model) worktreeEntries() []wtEntry {
	byDir := map[string][]domain.SessionInfo{}
	for _, info := range domain.Sessions().List() {
		byDir[filepath.Clean(info.Dir)] = append(byDir[filepath.Clean(info.Dir)], info)
	}
	out := make([]wtEntry, 0, len(m.worktrees))
	for i, w := range m.worktrees {
		out = append(out, wtEntry{wt: i})
		for _, info := range byDir[filepath.Clean(w.Path)] {
			out = append(out, wtEntry{wt: i, sess: info.ID})
		}
	}
	return out
}

func sessionRowText(info domain.SessionInfo) string {
	if info.State == domain.SessionExited {
		return "  └ ○ " + info.Label + "  " + i18n.T("exited (%d)", info.ExitCode)
	}
	return "  └ ● " + info.Label + "  " + i18n.T("running %s", formatElapsed(time.Since(info.Started)))
}

// selectedSession resolves a session sub-row under the Worktrees cursor.
func (m Model) selectedSession() (domain.SessionInfo, bool) {
	e, ok := m.selectedWorktreeEntry()
	if !ok || e.sess == "" {
		return domain.SessionInfo{}, false
	}
	s, ok := domain.Sessions().Get(e.sess)
	if !ok {
		return domain.SessionInfo{}, false
	}
	return s.Info(), true
}

func (m Model) selectedWorktreeEntry() (wtEntry, bool) {
	idx := m.displayIndices(panelWorktrees)
	sel := m.sel[panelWorktrees]
	if sel < 0 || sel >= len(idx) {
		return wtEntry{}, false
	}
	ents := m.worktreeEntries()
	if idx[sel] >= len(ents) {
		return wtEntry{}, false
	}
	return ents[idx[sel]], true
}
```

`worktreeList` (viewstate.go) becomes entry-based:

```go
type worktreeList struct {
	items []model.Worktree
	ents  []wtEntry
	rows  []string // one per entry
	times map[string]int64
}

func (l worktreeList) Len() int         { return len(l.ents) }
func (l worktreeList) Row(i int) string { return l.rows[i] }
// Name/Date of a session row are its worktree's, so a stable sort keeps it
// directly under its parent.
func (l worktreeList) Name(i int) string { /* existing body using l.items[l.ents[i].wt] */ }
func (l worktreeList) Date(i int) int64  { return l.times[l.items[l.ents[i].wt].Head] }
func (l worktreeList) Key(i int) string {
	k := l.items[l.ents[i].wt].Path
	if s := l.ents[i].sess; s != "" {
		k += "\x00" + string(s)
	}
	return k
}
// Haystack: a session row matches what its worktree matches (plus its own
// label), so a / filter never strands a sub-row without its parent.
func (l worktreeList) Haystack(i int) string {
	return l.rows[l.parentRow(i)] + " " + l.rows[i]
}
```

(`parentRow(i)` walks back to the entry with the same `wt` and `sess == ""`.) `listFor(panelWorktrees)` builds `ents := m.worktreeEntries()` and `rows` via `worktreeRows(ents)` — change `worktreeRows()` to take the entries: worktree entries render as today, session entries via `sessionRowText`. `backingIndex`: for `panelWorktrees` translate `u` → `ents[u]`; `ok=false` when `ents[u].sess != ""`, else return `ents[u].wt`. `selection.go` `case panelWorktrees:` → use `m.listFor(p).Key(u)` (entry-aware). `find_current.go`: its current-row lookup must compare against worktree entries only (read it; if it matches rows via `Key`, the `\x00` suffix already keeps session rows from matching). `action_menu.go:696` already goes through `backingIndex` (ok=false on session rows). In `model.go`'s `case "enter":` add BEFORE the `canEnterWorktree` check:

```go
			if m.focus == panelWorktrees {
				if info, ok := m.selectedSession(); ok {
					nm, cmd := m.openConsole(info.ID)
					return nm, cmd
				}
			}
```

- [ ] **Step 4: Run — expect PASS**; then the whole `internal/tui` package (`go test ./internal/tui/ > log`) since every worktree test rides this list.
- [ ] **Step 5: Commit** `feat(tui): agent sessions as sub-rows under their worktree`.

---

### Task 6: Start agent… (first-run detect, chooser, approval, start) and session-row menu actions

**Files:**
- Create: `internal/tui/agent_start_popup.go`
- Modify: `internal/tui/action_menu.go` (direct-run rows after the copy rows block, `m.sessionMenuRows()`)
- Test: `internal/tui/agent_start_popup_test.go`

**Interfaces:**
- Consumes: `domain.SessionCommands(cfg, "tui")`, `domain.EnsureSessionCommands(cfg, globalPath, detect)`, `config.DefaultGlobalPath()`, `config.Load`, `exttool.Detect`, `m.toolCommandApproved` / `m.rememberToolApproval` / `approvalBoxView`, `m.svc.StartSession`, `openConsole`.
- Produces:
  ```go
  type agentStartPopup struct {
      stage    agentStage // stageDetecting | stageChoose | stageApprove
      worktree string     // cwd of the new session
      cmds     []config.ToolCommand
      sel      int
      pick     config.ToolCommand
  }
  type agentEnsureMsg struct{ added []string; cfg config.Config; err error; worktree string }
  type agentStartedMsg struct{ id domain.SessionID; err error }
  func (m Model) startAgentFor(worktree string) (Model, tea.Cmd)
  func (m Model) sessionMenuRows() []actionRow
  // test seams
  var agentDetect = func() []exttool.Detection { home, _ := os.UserHomeDir(); return exttool.Detect(exec.LookPath, os.Stat, home) }
  var agentGlobalConfigPath = config.DefaultGlobalPath
  ```

- [ ] **Step 1: Failing tests** — (a) with no session commands and `agentDetect` returning claude, `startAgentFor` pushes the popup in `stageDetecting` and returns a cmd; feeding the `agentEnsureMsg` it produces shows `stageChoose` with "Claude" and sets `statusMsg` containing "Added Claude"; (b) with one approved command, choosing it starts a session whose console opens focused; (c) an unapproved command goes to `stageApprove`, `esc` cancels without starting; (d) `sessionMenuRows` on a worktree row has `start-agent`; on a running session row has `session-open`, `session-kill`; on an exited one `session-open`, `session-remove`; (e) nothing detected → status mentions `category = "session"` and the popup closes. Use `agentDetect`/`agentGlobalConfigPath` overrides pointing into `t.TempDir()`; serial tests (globals). Write them in the style of Task 3/5 tests with `startTestSession`'s manager setup for (b).

- [ ] **Step 2: Run — expect FAIL**.

- [ ] **Step 3: Implement** — the popup is a list layer (`renderWindow` + `popupBox`, per the adding-tui-windows skill). Stages:
  - `stageDetecting`: box text `i18n.T("Detecting installed agents…")` — the busy notice; keys: `esc` closes. The cmd runs `domain.EnsureSessionCommands(m.cfg, agentGlobalConfigPath(), agentDetect)` then `config.Load(agentGlobalConfigPath(), m.repoConfigPath)` off-thread → `agentEnsureMsg`.
  - On `agentEnsureMsg`: set `m.cfg = msg.cfg` when non-zero; `added != nil` → `m.statusMsg = i18n.T("Added %s to %s — edit there or in Settings → External tools", strings.Join(added, ", "), path)`; commands empty → close + `m.statusMsg = i18n.T("no agent found — add a [[tools.command]] block with category = \"session\" and mode = \"session\"")`; one command → go straight to approve/start; else `stageChoose`.
  - `stageChoose`: numbered rows (`1 Claude`, `2 Codex (yolo)`…); `j/k/↑/↓`, digits pick, `enter` pick, `esc` close.
  - Pick: approved (`m.toolCommandApproved(tc.Command)`) → start; else `stageApprove` rendering `i18n.T("Start this agent?  (%s)", tc.Name) + "\n\n" + approvalBoxView(tc.Command, w)`; `enter` → `m.rememberToolApproval(tc.Command)` + start; `esc` back to choose (or close when it was the only one).
  - Start: pop the popup, then a cmd calling `m.svc.StartSession(ctx, tc, worktree, cols, rows)` with the docked console's inner size (`consoleInner(m.consoleBox())` computed as if docked) → `agentStartedMsg`; on success `openConsole(id)`, on error `m.statusMsg = i18n.T("could not start %s: %s", name, err)`.
  - `startAgentFor(worktree)`: `cmds := domain.SessionCommands(m.cfg, "tui")`; empty → push `stageDetecting` + ensure cmd; else push choose (or straight to pick when exactly one).
- `sessionMenuRows()` (action_menu.go, appended after the row group, only when `m.focus == panelWorktrees`):
  - worktree row → `actionRow{id: "start-agent", label: i18n.T("Start agent…"), run: func(m Model) (tea.Model, tea.Cmd) { wt, _ := m.selectedWorktree(); return m.startAgentFor(wt.Path) }}`
  - session row → `session-open` "Open session", running: `session-kill` "Kill session" (runs `domain.Sessions().Kill(id)`), exited: `session-remove` "Remove session".
  - `menu_labels_test.go` may require every menu id be registered — follow what it reports.

- [ ] **Step 4: Run — expect PASS**.
- [ ] **Step 5: Commit** `feat(tui): Start agent… — first-run detect, chooser, approval, console`.

---

### Task 7: The `ctrl+\` sessions popup

**Files:**
- Create: `internal/tui/sessions_popup.go`
- Modify: `internal/tui/console.go` (`openSessionsPopup` real), `internal/tui/console_keys_route_test.go` (drop the Task 4 skip)
- Test: `internal/tui/sessions_popup_test.go`

**Interfaces:**
- Produces:
  ```go
  type sessionsPopup struct {
      quitMode bool
      sel      int
      query    string
      typing   bool
      mode     dispMode
      hscroll  int
      confirmKill domain.SessionID // "" = none; a running session asks once
  }
  func (m Model) openSessionsPopup(quitMode bool) (Model, tea.Cmd)
  func sessionsPopupRows(list []domain.SessionInfo, query string) (rows []string, ids []domain.SessionID) // group headers carry id ""
  ```

- [ ] **Step 1: Failing tests**: grouping (`repoA` header, `  worktree` sub-header, session rows) from a fabricated `[]domain.SessionInfo` (pure function — `t.Parallel()`); `enter` on a session row opens its console and pops the popup; `k` on a running row asks (`confirmKill`), second `k`/`y` kills; `x` on an exited row removes it, on a running row does nothing + status; `/` filters by label/worktree/repo; empty list shows `(no agent sessions)` and `esc` closes; `TestPopupOpensSessionFromOtherRepoWithoutReRoot` — a session whose `Dir` is outside `m.currentWorktree` opens without changing `m.currentWorktree`/`m.svc`.

- [ ] **Step 2: Run — expect FAIL**.

- [ ] **Step 3: Implement** a list popup per the skill (renderWindow, popupBox, `z`/shift-arrows, `[z] mode` hint). Rows: `repo` (bold), `  <worktree base>  <dir>`, `    ● Claude  running 12m` / `    ○ Codex  exited (0)`. Selection skips header rows. Footer hint: `i18n.T("[enter] open  [k] kill  [x] remove  [/] filter  [esc] close")`; in quit mode the popup title is `i18n.T("%d agent sessions running — quit gg?", n)` and the hint row gets `i18n.T("[Q] kill all and quit  [esc] cancel")` (Task 8 wires `Q`). `openSessionsPopup(false)` with zero sessions → status `i18n.T("no agent sessions — start one from a worktree's . menu")`, no popup.

- [ ] **Step 4: Run — expect PASS** (incl. `TestCtrlBackslashFromPanels`).
- [ ] **Step 5: Commit** `feat(tui): ctrl+\ sessions popup across repos and worktrees`.

---

### Task 8: Quit guard, exit notices, worktree-delete guard, shutdown safety net

**Files:**
- Modify: `internal/tui/run.go` (`tea.WithFilter(quitFilter)`, `KillAll` after `p.Run()`), `internal/tui/console.go` (`onSessionsChanged` notices), `internal/tui/sessions_popup.go` (`Q`), `internal/tui/model.go` (field `quitConfirmed bool`, `quitHeldMsg` arm), the worktree delete handler
- Test: `internal/tui/quit_guard_test.go`

**Interfaces:**
- Produces:
  ```go
  type quitHeldMsg struct{}
  func quitFilter(m tea.Model, msg tea.Msg) tea.Msg // QuitMsg + live sessions + !quitConfirmed → quitHeldMsg{}
  ```

- [ ] **Step 1: Failing tests**:
  - `TestQuitFilterHoldsQuitWithLiveSessions`: with a live session, `quitFilter(m, tea.QuitMsg{})` returns `quitHeldMsg{}`; with `m.quitConfirmed = true` it returns the `QuitMsg`; with no live sessions it returns the `QuitMsg`.
  - `TestQuitHeldOpensQuitModePopup`: `Update(quitHeldMsg{})` pushes `sessionsPopup{quitMode: true}`.
  - `TestKillAllAndQuit`: `Q` in quit mode sets `quitConfirmed`, and the returned cmd (run it) kills every session then yields `tea.QuitMsg`.
  - `TestExitNoticeWhenUnfocused`: a session that exits while no console shows it → after `sessionsChangedMsg`, `statusMsg` contains `exited (3)`; while its console is focused → no status notice.
  - `TestDeleteWorktreeWithRunningSessionRefused`: `d` on a worktree with a running session sets a status naming the agent and starts no op.

- [ ] **Step 2: Run — expect FAIL**.

- [ ] **Step 3: Implement**
  - `quitFilter` (run.go): `if _, ok := msg.(tea.QuitMsg); ok { if mm, ok := m.(Model); ok && !mm.quitConfirmed && domain.Sessions().LiveCount() > 0 { return quitHeldMsg{} } }; return msg`. `tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithFilter(quitFilter))`.
  - After `p.Run()` returns (any path): `ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second); domain.Sessions().KillAll(ctx); cancel()` — the safety net for panics / forced exits.
  - `quitHeldMsg` arm: `return m.openSessionsPopup(true)`.
  - `Q` in quit mode: `m.quitConfirmed = true`, popLayer, `return m, func() tea.Msg { ctx…; domain.Sessions().KillAll(ctx); return tea.QuitMsg{} }` with `m.statusMsg = i18n.T("ending agent sessions…")` (the busy notice).
  - `onSessionsChanged`: keep `m.sessionStates *map[domain.SessionID]domain.SessionState` (pointer field, value-receiver rule); for each listed session whose previous state was Running and now Exited, unless `m.console` shows it focused: `m.statusMsg = i18n.T("%s in %s exited (%d)", info.Label, filepath.Base(info.Dir), info.ExitCode)`. Re-arm `waitSessionsCmd()`.
  - Delete guard: in the worktree delete key/menu handler, before starting the op, `for _, info := range domain.Sessions().List() { if info.State == domain.SessionRunning && filepath.Clean(info.Dir) == filepath.Clean(wt.Path) { m.statusMsg = i18n.T("%s is running in this worktree — kill it first (ctrl+\\)", info.Label); return m, nil } }`. (Spec ruling: the spec offered *Kill session and delete / Cancel*; a refusal with the exact way out is the smaller change — ledger it.)

- [ ] **Step 4: Run — expect PASS**.
- [ ] **Step 5: Commit** `feat(tui): quit guard for live agent sessions, exit notices, delete guard`.

---

### Task 9: `[console]` keys config, help/footer, i18n bundles, docs, headless probe, gate

**Files:**
- Modify: `internal/config/config.go` (+ `ConsoleConfig{StepOutKey, SessionsKey string}` section `[console]`, defaults, overlay, Load wiring), `internal/config/template.go` (settingDocs), `internal/config/config_test.go`, `internal/tui/console.go` (`stepOutKey`/`sessionsKey` read `m.cfg.Console` with defaults), `internal/tui/footer.go`, `internal/tui/help.go`, `internal/i18n/lang/{ja,ko,zh,ru}.toml`, `README.md`, `CHANGELOG.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` (tui row mention)

- [ ] **Step 1: Config (adding-config-entries skill)** — failing table test `TestConsoleKeysLayers` (default `ctrl+]`/`ctrl+\`, global-only, repo-over-global, empty ignored); implement; `TestSettingDocsCoverAllFields` green.
- [ ] **Step 2: TUI reads config** — failing test: `m.cfg.Console.StepOutKey = "ctrl+q"` → `ctrl+q` steps out and `ctrl+]` is forwarded to the agent; implement `stepOutKey()`/`sessionsKey()` with fallback to defaults when empty.
- [ ] **Step 3: Help + footer** — global binding `{"sessions", "ctrl+\\", i18n.T("[ctrl+\\] agent sessions"), always, scopeGlobal}`; a console-focused footer `i18n.T("[ctrl+]] step out  [ctrl+\\] sessions")` (read `footer.go` for how a surface replaces the footer); Worktrees context binding for Start agent… if the menu test requires a key (otherwise the direct-run row suffices); `helpContent()` rows for both keys + the console states. Run `go test ./internal/tui/ -run 'Help|Footer|MenuLabel'`.
- [ ] **Step 4: i18n** — run `go test ./internal/tui/ -run 'I18n|Vocab|Engine'`; add every missing key to all four bundles with real translations (the adding-translations skill); re-run until green.
- [ ] **Step 5: Headless probe** (driving-tui-headless skill): build `gg` in the worktree, configure a scratch `XDG_CONFIG_HOME` with a session command `bash`, drive `./tui-capture.sh` on a scratch repo: `3` (Worktrees tab — use whatever key reaches it), `.`, pick Start agent…, type `echo PROBE-OK` + enter, capture (expect `PROBE-OK` in the Commits column), `ctrl+]`, capture (unfocused title hint), `esc`, capture (sub-row `└ ● bash`), `ctrl+\`, capture (popup), `esc`, `q`, capture (quit-mode popup). Record the captures' key lines in the ledger. Fix any defect found TDD-first.
- [ ] **Step 6: Docs** — CHANGELOG entry (user-facing: how to start, the two keys, the states, sub-rows, popup, quit guard, `[console]` keys), README (Worktrees/keys + config), `docs/CLAUDE-details.md` section (key-routing placement and why, `tea.WithFilter` quit guard, entry-based worktree list, repaint waiter + gen, right-column exclusivity), CLAUDE.md `tui` row: add "agent console (console.go)" in one clause.
- [ ] **Step 7: Full gate** — `./test.sh race` → all green; deliver the built `gg` binary path to the user.
- [ ] **Step 8: Commit** `docs+config: agent console keys, help, translations`.
