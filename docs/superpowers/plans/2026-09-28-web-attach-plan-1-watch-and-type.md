# Web attach — plan 1: watch and type

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo never uses subagents). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The gg web page lists the agent sessions this process owns, opens one as a live console painted from the server's screen, forwards typing to it, and owns the session size while its console is focused.

**Architecture:** `agentsession` gains a styled-run snapshot (`ScreenRuns`) and `Text`; `domain` maps the browser's key names to emulator key events and clamps sizes; `web` adds `GET /api/sessions`, `GET /api/tasks`, a per-console SSE stream fed by one frame producer per session (row-diffed per stream), `POST /api/session-input` and `/api/session-size`, a `sessions` live-hub event, and `static/console.js` (painter + layer). `ctrl+\` becomes the tabbed switcher (Agents · AI tasks · Open files); the sidebar shows session sub-rows. The TUI stops pushing an unfocused console's size on window resize and pushes it on focus gain.

**Tech Stack:** Go 1.26 stdlib HTTP + SSE, `charmbracelet/ultraviolet` cells (via `x/vt`), vanilla ES modules, JS-in-Go tests under node, playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-09-28-web-attach-design.md`

## Global Constraints

- `web`, `tui`, `cli`, `mcp` never import `internal/agentsession` (archtest); everything goes through `domain` aliases.
- Every POST is behind `writeGuard` (JSON content type + loopback Origin); every session id is validated against `domain.Sessions().Get` before work.
- Console size clamp: cols 20..500, rows 5..300.
- Frame producer coalesce: 40 ms; a stream that cannot take a frame is skipped and gets a full frame next.
- The page paints only what the server sends: no escape bytes on the wire, runs carry only non-default attributes, colours are `#rrggbb`.
- Reserved keys: `ctrl+]` step out, `ctrl+\` switcher. ctrl+w/t/n, ctrl+tab, ctrl+shift+*, F5/F11/F12 are never captured or faked.
- Only a focused web console posts its size. TUI: no push on window resize while unfocused; push on focus gain.
- Paths are cut in the middle (`elidePath`); key hints only in `#foot`; the web hides by id (`#id.hidden`); a `\` inside a JS template literal is written `\\`.
- Run `go test ./internal/web/` after every task that touches `internal/web`; `./test.sh` and `./test.sh race` before the merge; never edit the tree while `./test.sh` runs.
- Docs: CHANGELOG (always), README (the web console), `docs/CLAUDE-details.md` (frame protocol, size ownership), memory `agent-sessions-feature.md` — last task.

## Review Focus

1. **A wide glyph at the row's end** (a `你` in column cols−1 whose right half is clipped): `ScreenRuns` must emit one run and never a phantom cell — Task 1 pins it.
2. **Two streams on one session, one slow**: the slow one must get a full frame after being skipped, never a partial diff against a frame it never saw — Task 4 pins it.
3. **Keys with text longer than one rune** (IME commit, batched runes): `ConsoleKeyEvent` sends text through `SendText`, not rune-by-rune — Task 2 and Task 6 pin it.
4. **A size POST for a session shown in the TUI too**: the TUI must not fight it on its next window-size message while its console is unfocused — Task 9 pins it.
5. **A session that exits while a stream is attached**: the stream sends `exited` once, the last screen stays, and a later `Remove` sends `gone` — Task 4 pins it.

---

### Task 1: `ScreenRuns` and `Text` on the session

**Files:**
- Create: `internal/agentsession/runs.go`
- Modify: `internal/agentsession/session.go` (export `Text`)
- Test: `internal/agentsession/runs_test.go`

**Interfaces:**
- Produces:
  ```go
  type Run struct {
      Text   string `json:"t"`
      Fg, Bg string `json:"fg,omitempty"`,`json:"bg,omitempty"` // "#rrggbb"; "" = default
      Bold, Italic, Underline, Reverse, Dim, Strike bool // json b i u r d s, omitempty
  }
  type RunRow struct{ Runs []Run }             // json "runs" (an empty row = [])
  type ScreenRuns struct {
      Lines            []RunRow // exactly Rows long
      Cols, Rows       int
      CursorX, CursorY int
      CursorVisible    bool
      AltScreen        bool
  }
  func (s *Session) ScreenRuns() ScreenRuns
  func (s *Session) Text() string  // today's screenText, exported
  ```

- [ ] **Step 1: Write the failing test**

```go
// internal/agentsession/runs_test.go
package agentsession

import (
	"strings"
	"testing"
	"time"
)

func waitText(t *testing.T, s *Session, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.Text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, s.Text())
}

func TestScreenRunsMergesEqualStylesAndColours(t *testing.T) {
	t.Parallel()
	s := startSh(t, `printf '\033[1;31mred\033[0m plain'; sleep 5`)
	waitText(t, s, "red plain")
	sr := s.ScreenRuns()
	if sr.Cols != 40 || sr.Rows != 10 || len(sr.Lines) != 10 {
		t.Fatalf("size %dx%d lines=%d", sr.Cols, sr.Rows, len(sr.Lines))
	}
	row := sr.Lines[0].Runs
	if len(row) < 2 || row[0].Text != "red" || !row[0].Bold || row[0].Fg == "" || row[0].Fg[0] != '#' || len(row[0].Fg) != 7 {
		t.Fatalf("first run = %+v (row %+v)", row[0], row)
	}
	if row[1].Bold || row[1].Fg != "" || !strings.HasPrefix(row[1].Text, " plain") {
		t.Fatalf("second run = %+v", row[1])
	}
	if sr.CursorY != 0 || sr.CursorX != len("red plain") || !sr.CursorVisible || sr.AltScreen {
		t.Fatalf("cursor (%d,%d) vis=%v alt=%v", sr.CursorX, sr.CursorY, sr.CursorVisible, sr.AltScreen)
	}
}

func TestScreenRunsWideGlyphIsOneRunWithoutAPhantomCell(t *testing.T) {
	t.Parallel()
	s := startSh(t, `printf '\033[38G你'; sleep 5`) // column 38 of 40: the glyph fills 38–39
	waitText(t, s, "你")
	row := s.ScreenRuns().Lines[0].Runs
	joined := ""
	for _, r := range row {
		joined += r.Text
	}
	if !strings.HasSuffix(joined, "你") || strings.Count(joined, "你") != 1 {
		t.Fatalf("row text %q", joined)
	}
	// 37 spaces then the glyph: the right-half cell contributes nothing.
	if want := strings.Repeat(" ", 37) + "你"; joined != want {
		t.Fatalf("row = %q, want %q", joined, want)
	}
}

func TestScreenRunsBlankRowIsEmpty(t *testing.T) {
	t.Parallel()
	s := startSh(t, `printf 'x'; sleep 5`)
	waitText(t, s, "x")
	sr := s.ScreenRuns()
	if len(sr.Lines[5].Runs) != 0 {
		t.Fatalf("row 5 = %+v, want no runs", sr.Lines[5].Runs)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd internal/agentsession && go test -run 'TestScreenRuns' ./ 2>&1 | head -5`
Expected: FAIL to compile — `s.ScreenRuns undefined`, `s.Text undefined`.

- [ ] **Step 3: Implement**

```go
// internal/agentsession/runs.go
package agentsession

import (
	"fmt"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

// Run is a stretch of equally styled cells in one row. Only attributes that
// differ from the default are set, so the wire carries nothing for plain
// text. Colours are "#rrggbb" (palette indices resolved), "" = default.
type Run struct {
	Text      string `json:"t"`
	Fg        string `json:"fg,omitempty"`
	Bg        string `json:"bg,omitempty"`
	Bold      bool   `json:"b,omitempty"`
	Italic    bool   `json:"i,omitempty"`
	Underline bool   `json:"u,omitempty"`
	Reverse   bool   `json:"r,omitempty"`
	Dim       bool   `json:"d,omitempty"`
	Strike    bool   `json:"s,omitempty"`
}

// RunRow is one screen row. An empty Runs is a blank row.
type RunRow struct {
	Runs []Run `json:"runs"`
}

// ScreenRuns is the visible grid as styled runs — the web console's frame.
// The cursor is NOT painted into the runs (the page draws it), so one
// snapshot serves focused and unfocused viewers alike.
type ScreenRuns struct {
	Lines            []RunRow
	Cols, Rows       int
	CursorX, CursorY int
	CursorVisible    bool
	AltScreen        bool
}

// ScreenRuns snapshots the grid under the session lock (CellAt hands out a
// live pointer). Adjacent cells of equal style merge into one run; a wide
// glyph's right-half cell (zero, after a Width>1 cell) is skipped.
func (s *Session) ScreenRuns() ScreenRuns {
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	w, h := s.emu.Width(), s.emu.Height()
	out := ScreenRuns{Lines: make([]RunRow, h), Cols: w, Rows: h, AltScreen: s.emu.IsAltScreen()}
	pos := s.emu.CursorPosition()
	out.CursorX, out.CursorY, out.CursorVisible = pos.X, pos.Y, !s.cursorHidden.Load()
	for y := range h {
		runs := []Run{}
		var cur *Run
		var curStyle uv.Style
		skip := 0
		for x := range w {
			if skip > 0 {
				skip--
				continue
			}
			c := s.emu.CellAt(x, y)
			text, style := " ", uv.Style{}
			if c != nil && !c.IsZero() {
				text, style = c.Content, c.Style
				if c.Width > 1 {
					skip = c.Width - 1
				}
			}
			if cur != nil && curStyle.Equal(&style) {
				cur.Text += text
				continue
			}
			runs = append(runs, runFor(text, style))
			cur, curStyle = &runs[len(runs)-1], style
		}
		// Trailing default-styled blanks are noise: drop them, and drop the
		// whole row when nothing styled or visible is left.
		for len(runs) > 0 {
			last := runs[len(runs)-1]
			if last != (Run{Text: last.Text}) || len(trimRight(last.Text)) > 0 {
				break
			}
			runs = runs[:len(runs)-1]
		}
		if n := len(runs); n > 0 {
			last := &runs[n-1]
			if *last == (Run{Text: last.Text}) {
				last.Text = trimRight(last.Text)
			}
		}
		out.Lines[y] = RunRow{Runs: runs}
	}
	return out
}

func trimRight(s string) string {
	i := len(s)
	for i > 0 && s[i-1] == ' ' {
		i--
	}
	return s[:i]
}

func runFor(text string, st uv.Style) Run {
	return Run{
		Text: text, Fg: hexColor(st.Fg), Bg: hexColor(st.Bg),
		Bold: st.Attrs&uv.AttrBold != 0, Italic: st.Attrs&uv.AttrItalic != 0,
		Underline: st.Underline != uv.UnderlineNone, Reverse: st.Attrs&uv.AttrReverse != 0,
		Dim: st.Attrs&uv.AttrFaint != 0, Strike: st.Attrs&uv.AttrStrikethrough != 0,
	}
}

// hexColor renders any colour (basic, indexed, true) as "#rrggbb"; nil is
// the default and renders as "".
func hexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// Text is the visible grid as plain text, one line per row.
func (s *Session) Text() string { return s.screenText() }
```

Note: `uv.Style.Equal` takes a pointer receiver on both sides — write `(&curStyle).Equal(&style)` if the compiler complains.

- [ ] **Step 4: Run the tests**

Run: `cd internal/agentsession && go test ./ 2>&1 | tail -3`
Expected: `ok` (the three new tests plus the existing ones).

- [ ] **Step 5: Commit**

```bash
git add internal/agentsession/runs.go internal/agentsession/runs_test.go
git commit -m "feat(agentsession): ScreenRuns styled-run snapshot and Text for the web console"
```

---

### Task 2: domain — aliases, the console key table, the size clamp

**Files:**
- Create: `internal/domain/console_keys.go`
- Modify: `internal/domain/sessions.go` (aliases)
- Test: `internal/domain/console_keys_test.go`

**Interfaces:**
- Consumes: Task 1's `agentsession.ScreenRuns`, `Run`, `RunRow`.
- Produces:
  ```go
  type ScreenRuns = agentsession.ScreenRuns; type ScreenRun = agentsession.Run; type ScreenRunRow = agentsession.RunRow
  type ConsoleKey struct { K string `json:"k"`; Mod int `json:"mod"`; Text string `json:"text"` }
  const ModShift, ModCtrl, ModAlt = 1, 2, 4
  // ConsoleInput is what one wire key becomes: literal text (SendText) or a key event (SendKey).
  type ConsoleInput struct { Text string; Key SessionKey; IsKey bool }
  func ConsoleKeyEvent(k ConsoleKey) (ConsoleInput, error)
  func ClampConsoleSize(cols, rows int) (int, int)
  ```

- [ ] **Step 1: Write the failing test**

```go
// internal/domain/console_keys_test.go
package domain

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestConsoleKeyEventMapsNamesAndModifiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   ConsoleKey
		code rune
		mod  uv.KeyMod
	}{
		{ConsoleKey{K: "enter"}, uv.KeyEnter, 0},
		{ConsoleKey{K: "tab", Mod: ModShift}, uv.KeyTab, uv.ModShift},
		{ConsoleKey{K: "esc"}, uv.KeyEscape, 0},
		{ConsoleKey{K: "backspace"}, uv.KeyBackspace, 0},
		{ConsoleKey{K: "up", Mod: ModCtrl}, uv.KeyUp, uv.ModCtrl},
		{ConsoleKey{K: "f5"}, uv.KeyF5, 0},
		{ConsoleKey{K: "char", Mod: ModCtrl, Text: "c"}, 'c', uv.ModCtrl},
		{ConsoleKey{K: "char", Mod: ModAlt, Text: "x"}, 'x', uv.ModAlt},
	}
	for _, c := range cases {
		in, err := ConsoleKeyEvent(c.in)
		if err != nil || !in.IsKey {
			t.Fatalf("%+v: err=%v in=%+v", c.in, err, in)
		}
		kp, ok := in.Key.(uv.KeyPressEvent) // SessionKey is the uv.KeyEvent interface
		if !ok || kp.Code != c.code || kp.Mod != c.mod {
			t.Fatalf("%+v → %#v, want code %q mod %v", c.in, in.Key, c.code, c.mod)
		}
	}
}

func TestConsoleKeyEventPlainTextIsSentAsText(t *testing.T) {
	t.Parallel()
	for _, txt := range []string{"a", "你好", "hello"} { // an IME commit or batched runes stay one string
		in, err := ConsoleKeyEvent(ConsoleKey{K: "char", Text: txt})
		if err != nil || in.IsKey || in.Text != txt {
			t.Fatalf("%q → %+v err=%v", txt, in, err)
		}
	}
}

func TestConsoleKeyEventRefusesUnknownAndEmpty(t *testing.T) {
	t.Parallel()
	for _, k := range []ConsoleKey{{K: "bogus"}, {K: "char"}, {K: "char", Mod: ModCtrl, Text: "ab"}, {K: "enter", Mod: 99}} {
		if _, err := ConsoleKeyEvent(k); err == nil {
			t.Fatalf("%+v accepted", k)
		}
	}
}

func TestClampConsoleSize(t *testing.T) {
	t.Parallel()
	for _, c := range [][4]int{{0, 0, 20, 5}, {80, 24, 80, 24}, {9999, 9999, 500, 300}, {-3, 40, 20, 40}} {
		if w, h := ClampConsoleSize(c[0], c[1]); w != c[2] || h != c[3] {
			t.Fatalf("%dx%d → %dx%d, want %dx%d", c[0], c[1], w, h, c[2], c[3])
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd internal/domain && go test -run 'TestConsoleKey|TestClampConsole' ./ 2>&1 | head -5`
Expected: FAIL to compile — `ConsoleKey` undefined.

- [ ] **Step 3: Implement**

Add to `internal/domain/sessions.go`'s alias block:

```go
	ScreenRuns   = agentsession.ScreenRuns
	ScreenRun    = agentsession.Run
	ScreenRunRow = agentsession.RunRow
```

```go
// internal/domain/console_keys.go
package domain

import (
	"errors"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
)

// ConsoleKey is one keystroke as the web page sends it: an allowlisted
// name, a modifier mask, and the text for "char". The TUI maps Bubble Tea
// keys to the same emulator events (console_keys.go there); both frontends
// therefore give the child identical bytes for the same key.
type ConsoleKey struct {
	K    string `json:"k"`
	Mod  int    `json:"mod"`
	Text string `json:"text"`
}

// Modifier bits of ConsoleKey.Mod.
const (
	ModShift = 1
	ModCtrl  = 2
	ModAlt   = 4
	modAll   = ModShift | ModCtrl | ModAlt
)

// ConsoleInput is a decoded ConsoleKey: literal text for SendText, or a key
// event for SendKey (the emulator encodes it for the child's modes).
type ConsoleInput struct {
	Text  string
	Key   SessionKey
	IsKey bool
}

var consoleKeyNames = map[string]rune{
	"enter": uv.KeyEnter, "tab": uv.KeyTab, "esc": uv.KeyEscape, "backspace": uv.KeyBackspace,
	"delete": uv.KeyDelete, "insert": uv.KeyInsert,
	"up": uv.KeyUp, "down": uv.KeyDown, "left": uv.KeyLeft, "right": uv.KeyRight,
	"home": uv.KeyHome, "end": uv.KeyEnd, "pgup": uv.KeyPgUp, "pgdn": uv.KeyPgDown,
	"f1": uv.KeyF1, "f2": uv.KeyF2, "f3": uv.KeyF3, "f4": uv.KeyF4, "f5": uv.KeyF5, "f6": uv.KeyF6,
	"f7": uv.KeyF7, "f8": uv.KeyF8, "f9": uv.KeyF9, "f10": uv.KeyF10, "f11": uv.KeyF11, "f12": uv.KeyF12,
}

func uvMod(mod int) uv.KeyMod {
	var m uv.KeyMod
	if mod&ModShift != 0 {
		m |= uv.ModShift
	}
	if mod&ModCtrl != 0 {
		m |= uv.ModCtrl
	}
	if mod&ModAlt != 0 {
		m |= uv.ModAlt
	}
	return m
}

// ConsoleKeyEvent decodes one wire key. Plain text (no ctrl/alt) is sent as
// text so an IME commit or a burst of runes stays one write; a ctrl/alt
// letter is a key event with exactly one rune.
func ConsoleKeyEvent(k ConsoleKey) (ConsoleInput, error) {
	if k.Mod&^modAll != 0 {
		return ConsoleInput{}, errors.New("unknown modifier")
	}
	if k.K == "char" {
		if k.Text == "" {
			return ConsoleInput{}, errors.New("char without text")
		}
		if k.Mod&(ModCtrl|ModAlt) == 0 {
			return ConsoleInput{Text: k.Text}, nil
		}
		if utf8.RuneCountInString(k.Text) != 1 {
			return ConsoleInput{}, errors.New("a modified char is one rune")
		}
		r, _ := utf8.DecodeRuneInString(k.Text)
		return ConsoleInput{IsKey: true, Key: uv.KeyPressEvent{Code: r, Mod: uvMod(k.Mod)}}, nil
	}
	code, ok := consoleKeyNames[k.K]
	if !ok {
		return ConsoleInput{}, errors.New("unknown key " + k.K)
	}
	return ConsoleInput{IsKey: true, Key: uv.KeyPressEvent{Code: code, Mod: uvMod(k.Mod)}}, nil
}

// ClampConsoleSize bounds a viewer's cols×rows so a stray request can never
// allocate a huge emulator or a useless one.
func ClampConsoleSize(cols, rows int) (int, int) {
	return min(max(cols, 20), 500), min(max(rows, 5), 300)
}
```

`SessionKey` is `uv.KeyEvent` (an interface, `event.go:227`); `uv.KeyPressEvent` satisfies it and is what the TUI's `consoleSpecial` values are.

- [ ] **Step 4: Run the tests**

Run: `cd internal/domain && go test -run 'TestConsoleKey|TestClampConsole' ./ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Run the archtest and the whole domain package**

Run: `go test ./internal/archtest/ ./internal/domain/ 2>&1 | tail -3`
Expected: `ok` for both.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/console_keys.go internal/domain/console_keys_test.go internal/domain/sessions.go
git commit -m "feat(domain): web console key table, size clamp, ScreenRuns aliases"
```

---

### Task 3: `GET /api/sessions`, `GET /api/tasks`, and the `sessions` live event

**Files:**
- Create: `internal/web/sessions_http.go`
- Modify: `internal/web/live.go` (`liveMsg.Sessions`), `internal/web/server.go` (start/stop the watcher in `New`/`Close`)
- Test: `internal/web/sessions_http_test.go`

**Interfaces:**
- Produces:
  ```go
  type sessionWire struct {
      ID string `json:"id"`; Label string `json:"label"`; Agent string `json:"agent"`; Repo string `json:"repo"`
      Worktree string `json:"worktree"`; State string `json:"state"`; ExitCode int `json:"exit_code"`
      Started time.Time `json:"started"`; Task string `json:"task,omitempty"`
      AgentState string `json:"agent_state,omitempty"`; Since time.Time `json:"since,omitempty"` // plan 3 fills these
  }
  type taskWire struct { ID, Key, Kind, Agent, Mode, State, Session string; Submitted, Started, Ended time.Time }
  func sessionsWire(list []domain.SessionInfo, tasks []domain.TaskInfo) []sessionWire
  func (s *Server) broadcastSessions()  // liveMsg{Reason:"sessions", Sessions: ...} via fanOut (bypasses the op gate)
  ```
  `state` is `"running"` or `"exited"`; `task` is the task id when an interactive task owns the session (its key becomes the label the page shows: `Claude · commit message`).

- [ ] **Step 1: Write the failing test**

```go
// internal/web/sessions_http_test.go
package web

import (
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
)

// testSession starts `sh -c script` under a private manager and returns it.
func testSession(t *testing.T, script string) *domain.AgentSession {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })
	s, err := domain.Sessions().Start(domain.SessionStartSpec{Label: "sh", AgentID: "sh", Repo: "r", Dir: t.TempDir(), Argv: []string{"sh", "-c", script}, Cols: 40, Rows: 10})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionsListsEverySession(t *testing.T) {
	s := testSession(t, "sleep 5")
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var body struct{ Sessions []sessionWire }
	if code := getJSON(t, ts, "/api/sessions", &body); code != http.StatusOK || len(body.Sessions) != 1 {
		t.Fatalf("code=%d body=%+v", code, body)
	}
	got := body.Sessions[0]
	if got.ID != string(s.Info().ID) || got.Label != "sh" || got.State != "running" || got.Worktree != s.Info().Dir || got.Repo != "r" {
		t.Fatalf("%+v", got)
	}
}

func TestSessionsWireMarksTaskOwnedSessions(t *testing.T) {
	t.Parallel()
	list := []domain.SessionInfo{{ID: "s1", Label: "Claude", State: domain.SessionRunning}, {ID: "s2", Label: "codex", State: domain.SessionExited, ExitCode: 3}}
	tasks := []domain.TaskInfo{{ID: "t9", Key: "commit message — main @ abc1234", Session: "s1", State: domain.TaskRunning}}
	w := sessionsWire(list, tasks)
	if w[0].Task != "t9" || w[0].Label != "Claude · commit message — main @ abc1234" || w[1].Task != "" || w[1].State != "exited" || w[1].ExitCode != 3 {
		t.Fatalf("%+v", w)
	}
}

func TestTasksListIsReadOnly(t *testing.T) {
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var body struct{ Tasks []taskWire }
	if code := getJSON(t, ts, "/api/tasks", &body); code != http.StatusOK || body.Tasks == nil {
		t.Fatalf("code=%d body=%+v", code, body)
	}
	if code, _ := postJSONRaw(t, ts, "/api/tasks", `{}`); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/tasks = %d, want 405", code)
	}
}

func TestSessionStartAndExitEmitSessionsEvents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	isolateGlobal(t)
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(t.Context())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	go func() {
		time.Sleep(200 * time.Millisecond)
		_, _ = domain.Sessions().Start(domain.SessionStartSpec{Label: "sh", Dir: t.TempDir(), Argv: []string{"sh", "-c", "exit 0"}, Cols: 20, Rows: 5})
	}()
	msgs := readLiveSSE(t, ts, 3, 5*time.Second) // hello, sessions (start), sessions (exit)
	if msgs[1].Reason != "sessions" || msgs[2].Reason != "sessions" || len(msgs[2].Sessions) != 1 || msgs[2].Sessions[0].State != "exited" {
		t.Fatalf("%+v", msgs)
	}
}
```

`domain.SessionStartSpec` does not exist yet: add `SessionStartSpec = agentsession.StartSpec` to the alias block in this task (the TUI builds specs inside domain today; the test needs the alias).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run 'TestSessions|TestTasksList|TestSessionStart' ./internal/web/ 2>&1 | head -5`
Expected: FAIL to compile — `sessionWire` undefined.

- [ ] **Step 3: Implement**

`internal/web/live.go`: add to `liveMsg`

```go
	// Sessions is the whole session list on Reason "sessions" (start, exit,
	// remove — and, from plan 3, an agent-state change). Bypasses the op
	// gate: a session exiting mid-op must still show.
	Sessions []sessionWire `json:"sessions,omitempty"`
```

```go
// internal/web/sessions_http.go
package web

import (
	"net/http"
	"sort"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/sessions", s.handleSessions)
		mux.HandleFunc("GET /api/tasks", s.handleTasks)
	})
}

// sessionWire is one session as the page sees it. agent_state/since are
// plan 3's activity detection and stay empty until then.
type sessionWire struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	Agent      string    `json:"agent"`
	Repo       string    `json:"repo"`
	Worktree   string    `json:"worktree"`
	State      string    `json:"state"`
	ExitCode   int       `json:"exit_code"`
	Started    time.Time `json:"started"`
	Task       string    `json:"task,omitempty"`
	AgentState string    `json:"agent_state,omitempty"`
	Since      time.Time `json:"since,omitempty"`
}

type taskWire struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Kind      string    `json:"kind"`
	Agent     string    `json:"agent"`
	Mode      string    `json:"mode"`
	State     string    `json:"state"`
	Session   string    `json:"session,omitempty"`
	Submitted time.Time `json:"submitted"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended"`
}

// sessionsWire joins the session list with the tasks that own sessions: a
// task-backed session is labelled "<agent> · <task key>" like the TUI's
// sub-row. Order: start time.
func sessionsWire(list []domain.SessionInfo, tasks []domain.TaskInfo) []sessionWire {
	owner := map[domain.SessionID]domain.TaskInfo{}
	for _, tk := range tasks {
		if tk.Session != "" {
			owner[tk.Session] = tk
		}
	}
	out := make([]sessionWire, 0, len(list))
	for _, info := range list {
		w := sessionWire{ID: string(info.ID), Label: info.Label, Agent: info.AgentID, Repo: info.Repo, Worktree: info.Dir,
			State: "running", ExitCode: info.ExitCode, Started: info.Started}
		if info.State == domain.SessionExited {
			w.State = "exited"
		}
		if tk, ok := owner[info.ID]; ok {
			w.Task, w.Label = string(tk.ID), info.Label+" · "+tk.Key
		}
		out = append(out, w)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

func tasksWire(list []domain.TaskInfo) []taskWire {
	out := make([]taskWire, 0, len(list))
	for _, tk := range list {
		out = append(out, taskWire{ID: string(tk.ID), Key: tk.Key, Kind: string(tk.Kind), Agent: tk.Agent, Mode: string(tk.Mode),
			State: string(tk.State), Session: string(tk.Session), Submitted: tk.Submitted, Started: tk.Started, Ended: tk.Ended})
	}
	return out
}

func (s *Server) sessionsNow() []sessionWire {
	return sessionsWire(domain.Sessions().List(), domain.Tasks().List())
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"sessions": s.sessionsNow()})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"tasks": tasksWire(domain.Tasks().List())})
}

// broadcastSessions tells every tab the list changed. fanOut, not emit: the
// op gate must not swallow a session's exit.
func (s *Server) broadcastSessions() {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "sessions", Sessions: s.sessionsNow()})
	}
}

// watchSessions forwards the manager's coalesced change signal to the tabs
// until stop closes. Started by New, stopped by Close.
func (s *Server) watchSessions(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case <-domain.Sessions().Changed():
			s.broadcastSessions()
		}
	}
}
```

Routes register through `RegisterRoutes` in an `init()`, as `openfiles_http.go` does. `server.go`: in `New`, add `s.sessionsStop = make(chan struct{}); go s.watchSessions(s.sessionsStop)`; in `Close`, `s.stopSessionsOnce.Do(func() { close(s.sessionsStop) })` with the two fields on `Server`. `domain.Tasks()` is process-global and lazily created, so calling it from the web is safe.

- [ ] **Step 4: Run the tests**

Run: `go test -run 'TestSessions|TestTasksList|TestSessionStart' ./internal/web/ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Run the whole web package**

Run: `go test ./internal/web/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/web/sessions_http.go internal/web/sessions_http_test.go internal/web/live.go internal/web/server.go internal/domain/sessions.go
git commit -m "feat(web): GET /api/sessions, GET /api/tasks, the sessions live event"
```

---

### Task 4: the screen stream — frame producer, row differ, `GET /api/session-screen`

**Files:**
- Create: `internal/web/console_stream.go`
- Test: `internal/web/console_stream_test.go`

**Interfaces:**
- Consumes: `domain.ScreenRuns`, `domain.Sessions().Get`.
- Produces:
  ```go
  type frameLine struct { Y int `json:"y"`; Runs []domain.ScreenRun `json:"runs"` }
  type frameWire struct {
      Full bool `json:"full"`; Cols, Rows, CX, CY int; Cursor, Alt bool  // json cols rows cx cy cursor alt
      Lines []frameLine `json:"lines"`
  }
  func diffFrame(prev *domain.ScreenRuns, next domain.ScreenRuns) frameWire  // prev nil or size differs → Full
  type screenFeeds struct{ ... }  // s.feeds: per-session producer registry
  func (f *screenFeeds) attach(id domain.SessionID, sess *domain.AgentSession) (<-chan feedMsg, func())
  type feedMsg struct { Screen *domain.ScreenRuns; Exited *int; Gone bool }
  ```
  SSE events: `hello` `{frame, palette}` where `palette` is the 16 basic colours as `#rrggbb` from `x/ansi`'s table (index 0..15); `frame`; `exited` `{"code":N}`; `gone` `{}`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/web/console_stream_test.go
package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func runs(texts ...string) domain.ScreenRunRow {
	r := domain.ScreenRunRow{Runs: []domain.ScreenRun{}}
	for _, t := range texts {
		r.Runs = append(r.Runs, domain.ScreenRun{Text: t})
	}
	return r
}

func TestDiffFrameSendsOnlyChangedRowsAndFullOnResize(t *testing.T) {
	t.Parallel()
	a := domain.ScreenRuns{Cols: 10, Rows: 3, Lines: []domain.ScreenRunRow{runs("a"), runs("b"), runs()}}
	f := diffFrame(nil, a)
	if !f.Full || len(f.Lines) != 3 || f.Cols != 10 {
		t.Fatalf("first frame = %+v", f)
	}
	b := a
	b.Lines = []domain.ScreenRunRow{runs("a"), runs("B"), runs()}
	b.CursorX = 4
	f = diffFrame(&a, b)
	if f.Full || len(f.Lines) != 1 || f.Lines[0].Y != 1 || f.Lines[0].Runs[0].Text != "B" || f.CX != 4 {
		t.Fatalf("partial = %+v", f)
	}
	c := b
	c.Cols = 12
	if f = diffFrame(&b, c); !f.Full || len(f.Lines) != 3 {
		t.Fatalf("resize frame = %+v", f)
	}
	if f = diffFrame(&c, c); f.Full || len(f.Lines) != 0 {
		t.Fatalf("same frame = %+v", f)
	}
}

type sseEvent struct {
	Name string
	Data string
}

// readConsoleSSE reads up to n events (or until timeout) from the session stream.
func readConsoleSSE(t *testing.T, ts *httptest.Server, id string, n int, timeout time.Duration) []sseEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/session-screen?id="+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() && len(out) < n {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.Name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.Data = strings.TrimPrefix(line, "data: ")
		case line == "" && cur.Name != "":
			out = append(out, cur)
			cur = sseEvent{}
		}
	}
	return out
}

func TestSessionScreenHelloThenFramesThenExited(t *testing.T) {
	s := testSession(t, `printf 'FIRST'; sleep 1; printf ' SECOND'; sleep 1; exit 7`)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	evs := readConsoleSSE(t, ts, string(s.Info().ID), 6, 8*time.Second)
	if len(evs) == 0 || evs[0].Name != "hello" {
		t.Fatalf("events %+v", evs)
	}
	var hello struct {
		Frame   frameWire `json:"frame"`
		Palette []string  `json:"palette"`
	}
	if err := json.Unmarshal([]byte(evs[0].Data), &hello); err != nil || !hello.Frame.Full || len(hello.Palette) != 16 || hello.Frame.Cols != 40 {
		t.Fatalf("hello %s err=%v", evs[0].Data, err)
	}
	sawSecond, sawExit := false, false
	for _, e := range evs[1:] {
		if e.Name == "frame" && strings.Contains(e.Data, "SECOND") {
			sawSecond = true
		}
		if e.Name == "exited" && strings.Contains(e.Data, `"code":7`) {
			sawExit = true
		}
	}
	if !sawSecond || !sawExit {
		t.Fatalf("second=%v exit=%v events=%+v", sawSecond, sawExit, evs)
	}
}

func TestSessionScreenRefusesUnknownID(t *testing.T) {
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	resp, err := http.Get(ts.URL + "/api/session-screen?id=nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
}

func TestFeedSkipsASlowStreamAndSendsItAFullFrameNext(t *testing.T) {
	t.Parallel()
	f := newScreenFeeds()
	fast, cancelFast := f.subscribe("x")
	slow, cancelSlow := f.subscribe("x")
	defer cancelFast()
	defer cancelSlow()
	a := domain.ScreenRuns{Cols: 5, Rows: 1, Lines: []domain.ScreenRunRow{runs("a")}}
	b := a
	b.Lines = []domain.ScreenRunRow{runs("b")}
	c := a
	c.Lines = []domain.ScreenRunRow{runs("c")}
	f.publish("x", a) // both take it
	<-fast
	f.publish("x", b) // fast takes it, slow still holds a → its buffer is full → skipped
	<-fast
	f.publish("x", c) // slow gets c, marked full
	<-fast
	first := <-slow
	if first.Screen.Lines[0].Runs[0].Text != "a" {
		t.Fatalf("slow first = %+v", first)
	}
	next := <-slow
	if next.Screen.Lines[0].Runs[0].Text != "c" || !next.Full {
		t.Fatalf("slow after skip = %+v, want c marked full", next)
	}
}

func TestSessionScreenGoneAfterRemove(t *testing.T) {
	s := testSession(t, `exit 0`)
	<-s.Done()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = domain.Sessions().Remove(s.Info().ID)
	}()
	evs := readConsoleSSE(t, ts, string(s.Info().ID), 3, 5*time.Second)
	if evs[len(evs)-1].Name != "gone" {
		t.Fatalf("events %+v", evs)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -run 'TestDiffFrame|TestSessionScreen|TestFeed' ./internal/web/ 2>&1 | head -5`
Expected: FAIL to compile — `diffFrame` undefined.

- [ ] **Step 3: Implement**

```go
// internal/web/console_stream.go
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/homeend/gigagit/internal/domain"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/session-screen", s.handleSessionScreen)
	})
}

// feedCoalesce bounds how often a session's screen is snapshotted.
const feedCoalesce = 40 * time.Millisecond

// --- wire ---------------------------------------------------------------------

type frameLine struct {
	Y    int                `json:"y"`
	Runs []domain.ScreenRun `json:"runs"`
}

// frameWire is one screen update. Full carries every row; otherwise only
// the rows that changed since the previous frame THIS stream wrote.
type frameWire struct {
	Full   bool        `json:"full"`
	Cols   int         `json:"cols"`
	Rows   int         `json:"rows"`
	CX     int         `json:"cx"`
	CY     int         `json:"cy"`
	Cursor bool        `json:"cursor"`
	Alt    bool        `json:"alt"`
	Lines  []frameLine `json:"lines"`
}

func frameHead(sr domain.ScreenRuns) frameWire {
	return frameWire{Cols: sr.Cols, Rows: sr.Rows, CX: sr.CursorX, CY: sr.CursorY, Cursor: sr.CursorVisible, Alt: sr.AltScreen, Lines: []frameLine{}}
}

// diffFrame builds the frame that turns prev into next: full when there is
// no prev or the size changed, else the changed rows only.
func diffFrame(prev *domain.ScreenRuns, next domain.ScreenRuns) frameWire {
	f := frameHead(next)
	full := prev == nil || prev.Cols != next.Cols || prev.Rows != next.Rows || len(prev.Lines) != len(next.Lines)
	f.Full = full
	for y, row := range next.Lines {
		if !full && slices.Equal(prev.Lines[y].Runs, row.Runs) {
			continue
		}
		f.Lines = append(f.Lines, frameLine{Y: y, Runs: nonNil(row.Runs)})
	}
	return f
}

func nonNil(r []domain.ScreenRun) []domain.ScreenRun {
	if r == nil {
		return []domain.ScreenRun{}
	}
	return r
}

// palette16 is the console's basic-colour table, sent once in hello so the
// page can theme "#rrggbb" values it recognises.
func palette16() []string {
	out := make([]string, 16)
	for i := range out {
		r, g, b, _ := ansi.BasicColor(i).RGBA()
		out[i] = fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	}
	return out
}

// --- feeds --------------------------------------------------------------------

// feedMsg is one delivery to a stream: a screen (Full when the stream missed
// one), an exit, or the session's removal.
type feedMsg struct {
	Screen *domain.ScreenRuns
	Full   bool
	Exited *int
	Gone   bool
}

type feedSub struct {
	ch     chan feedMsg
	missed bool // a publish found the buffer full: the next screen is marked Full
}

// screenFeeds fans one session's snapshots out to its attached streams. One
// producer goroutine per session runs while it has subscribers.
type screenFeeds struct {
	mu   sync.Mutex
	subs map[domain.SessionID]map[*feedSub]struct{}
	run  map[domain.SessionID]chan struct{} // producer stop channels
}

func newScreenFeeds() *screenFeeds {
	return &screenFeeds{subs: map[domain.SessionID]map[*feedSub]struct{}{}, run: map[domain.SessionID]chan struct{}{}}
}

// subscribe registers a stream for id (no producer: tests drive publish).
func (f *screenFeeds) subscribe(id domain.SessionID) (<-chan feedMsg, func()) {
	sub := &feedSub{ch: make(chan feedMsg, 1)}
	f.mu.Lock()
	if f.subs[id] == nil {
		f.subs[id] = map[*feedSub]struct{}{}
	}
	f.subs[id][sub] = struct{}{}
	f.mu.Unlock()
	return sub.ch, func() {
		f.mu.Lock()
		delete(f.subs[id], sub)
		last := len(f.subs[id]) == 0
		if last {
			delete(f.subs, id)
			if stop := f.run[id]; stop != nil {
				close(stop)
				delete(f.run, id)
			}
		}
		f.mu.Unlock()
	}
}

// publish hands sr to every subscriber of id; one that still holds an
// unread screen is skipped and gets its next screen marked Full.
func (f *screenFeeds) publish(id domain.SessionID, sr domain.ScreenRuns) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sub := range f.subs[id] {
		msg := feedMsg{Screen: &sr, Full: sub.missed}
		select {
		case sub.ch <- msg:
			sub.missed = false
		default:
			sub.missed = true
		}
	}
}

// send delivers a control message (exit, gone), waiting briefly so it is
// never lost behind an unread screen.
func (f *screenFeeds) send(id domain.SessionID, msg feedMsg) {
	f.mu.Lock()
	subs := make([]*feedSub, 0, len(f.subs[id]))
	for sub := range f.subs[id] {
		subs = append(subs, sub)
	}
	f.mu.Unlock()
	for _, sub := range subs {
		select {
		case sub.ch <- msg:
		case <-time.After(time.Second):
		}
	}
}

// attach subscribes and makes sure a producer runs for the session.
func (f *screenFeeds) attach(sess *domain.AgentSession) (<-chan feedMsg, func()) {
	id := sess.Info().ID
	ch, cancel := f.subscribe(id)
	f.mu.Lock()
	if f.run[id] == nil {
		stop := make(chan struct{})
		f.run[id] = stop
		go f.produce(sess, stop)
	}
	f.mu.Unlock()
	return ch, cancel
}

// produce snapshots the session on every change, coalesced, until stop; it
// reports the exit once and the removal (the manager no longer lists it).
// The manager's Changed() is ONE coalesced channel and watchSessions
// (sessions_http.go) is its only web receiver — a second receiver would
// steal its signals — so removal is polled once a second instead.
func (f *screenFeeds) produce(sess *domain.AgentSession, stop <-chan struct{}) {
	id := sess.Info().ID
	var timer *time.Timer
	var fire <-chan time.Time
	exited := false
	gone := time.NewTicker(time.Second)
	defer gone.Stop()
	for {
		select {
		case <-stop:
			return
		case <-sess.Changed():
			if timer == nil {
				timer = time.NewTimer(feedCoalesce)
				fire = timer.C
			}
		case <-fire:
			timer, fire = nil, nil
			sr := sess.ScreenRuns()
			f.publish(id, sr)
			if info := sess.Info(); info.State == domain.SessionExited && !exited {
				exited = true
				code := info.ExitCode
				f.send(id, feedMsg{Exited: &code})
			}
		case <-gone.C:
			if _, ok := domain.Sessions().Get(id); !ok {
				f.send(id, feedMsg{Gone: true})
				return
			}
		}
	}
}
```

`sess.Changed()` is per session and has one receiver here: the TUI waits on the same channel only while it shows that console (`waitSessionCmd`), and both receivers repaint from a fresh snapshot, so a stolen signal costs at most one 40 ms-late frame — acceptable, and the same as two TUI consoles would be.

Handler:

```go
func (s *Server) handleSessionScreen(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	sess, ok := domain.Sessions().Get(domain.SessionID(r.URL.Query().Get("id")))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no such session"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancel := s.feeds.attach(sess)
	defer cancel()
	last := sess.ScreenRuns()
	writeSSEEvent(w, "hello", map[string]any{"frame": diffFrame(nil, last), "palette": palette16()})
	fl.Flush()
	if info := sess.Info(); info.State == domain.SessionExited {
		writeSSEEvent(w, "exited", map[string]int{"code": info.ExitCode})
		fl.Flush()
	}
	ping := time.NewTicker(liveKeepalive)
	defer ping.Stop()
	for {
		select {
		case m := <-ch:
			switch {
			case m.Gone:
				writeSSEEvent(w, "gone", map[string]any{})
				fl.Flush()
				return
			case m.Exited != nil:
				writeSSEEvent(w, "exited", map[string]int{"code": *m.Exited})
			case m.Screen != nil:
				prev := &last
				if m.Full {
					prev = nil
				}
				// Always written: a frame with no rows still carries the
				// cursor, which moves without any cell changing.
				writeSSEEvent(w, "frame", diffFrame(prev, *m.Screen))
				last = *m.Screen
			}
			fl.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case <-s.closing:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, name string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, b)
}
```

`s.feeds` is a new `Server` field initialised in `New` (`feeds: newScreenFeeds()`).

- [ ] **Step 4: Run the tests**

Run: `go test -run 'TestDiffFrame|TestSessionScreen|TestFeed' ./internal/web/ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Run the whole web package, then with `-race` for these tests**

Run: `go test ./internal/web/ 2>&1 | tail -2 && go test -race -run 'TestSessionScreen|TestFeed' ./internal/web/ 2>&1 | tail -2`
Expected: `ok` twice.

- [ ] **Step 6: Commit**

```bash
git add internal/web/console_stream.go internal/web/console_stream_test.go internal/web/server.go
git commit -m "feat(web): per-console screen stream with a coalesced, row-diffed frame producer"
```

---

### Task 5: `POST /api/session-input` and `POST /api/session-size`

**Files:**
- Create: `internal/web/console_input.go`
- Test: `internal/web/console_input_test.go`

**Interfaces:**
- Consumes: Task 2's `domain.ConsoleKey`, `ConsoleKeyEvent`, `ClampConsoleSize`.
- Produces: `POST /api/session-input {id, keys:[ConsoleKey], paste}` → `{ok:true}`; `POST /api/session-size {id, cols, rows}` → `{cols, rows}` (the clamped size).

- [ ] **Step 1: Write the failing tests**

```go
// internal/web/console_input_test.go
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func waitSessionText(t *testing.T, s *domain.AgentSession, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.Text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, s.Text())
}

func TestSessionInputTypesKeysAndPaste(t *testing.T) {
	s := testSession(t, `read x; echo "GOT:$x"; read y; echo "PASTED:$y"; sleep 3`)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var out map[string]any
	body := `{"id":"` + string(s.Info().ID) + `","keys":[{"k":"char","text":"hi"},{"k":"char","mod":1,"text":"!"},{"k":"enter"}]}`
	if code := postJSON(t, ts, "/api/session-input", body, "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("code %d %v", code, out)
	}
	waitSessionText(t, s, "GOT:hi!")
	body = `{"id":"` + string(s.Info().ID) + `","paste":"pasted text\n"}`
	if code := postJSON(t, ts, "/api/session-input", body, "application/json", "", &out); code != http.StatusOK {
		t.Fatalf("code %d %v", code, out)
	}
	waitSessionText(t, s, "PASTED:pasted text")
}

func TestSessionInputRefusals(t *testing.T) {
	s := testSession(t, `exit 0`)
	<-s.Done()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var out map[string]any
	id := string(s.Info().ID)
	for _, c := range []struct {
		body, ctype, origin string
		want                int
	}{
		{`{"id":"` + id + `","keys":[{"k":"enter"}]}`, "application/json", "", http.StatusConflict}, // exited
		{`{"id":"nope","keys":[{"k":"enter"}]}`, "application/json", "", http.StatusNotFound},
		{`{"id":"` + id + `","keys":[{"k":"bogus"}]}`, "application/json", "", http.StatusBadRequest},
		{`{"id":"` + id + `"}`, "text/plain", "", http.StatusUnsupportedMediaType},
		{`{"id":"` + id + `"}`, "application/json", "http://evil.example", http.StatusForbidden},
	} {
		if code := postJSON(t, ts, "/api/session-input", c.body, c.ctype, c.origin, &out); code != c.want {
			t.Fatalf("%s → %d, want %d", c.body, code, c.want)
		}
	}
}

func TestSessionSizeClampsAndResizes(t *testing.T) {
	s := testSession(t, `sleep 5`)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var out struct{ Cols, Rows int }
	body := `{"id":"` + string(s.Info().ID) + `","cols":9999,"rows":2}`
	if code := postJSON(t, ts, "/api/session-size", body, "application/json", "", &out); code != http.StatusOK || out.Cols != 500 || out.Rows != 5 {
		t.Fatalf("code %d out %+v", code, out)
	}
	if sc := s.Screen(); sc.Cols != 500 || sc.Rows != 5 {
		t.Fatalf("emulator %dx%d", sc.Cols, sc.Rows)
	}
	if code := postJSON(t, ts, "/api/session-size", `{"id":"nope","cols":80,"rows":24}`, "application/json", "", &out); code != http.StatusNotFound {
		t.Fatalf("unknown id → %d", code)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -run 'TestSessionInput|TestSessionSize' ./internal/web/ 2>&1 | head -5`
Expected: FAIL — 404 for the routes (or compile errors if helpers are missing).

- [ ] **Step 3: Implement**

```go
// internal/web/console_input.go
package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/session-input", writeGuard(s.handleSessionInput))
		mux.HandleFunc("POST /api/session-size", writeGuard(s.handleSessionSize))
	})
}

type sessionInputReq struct {
	ID    string              `json:"id"`
	Keys  []domain.ConsoleKey `json:"keys"`
	Paste string              `json:"paste"`
}

// sessionByID answers 404 for an unknown id.
func sessionByID(w http.ResponseWriter, id string) (*domain.AgentSession, bool) {
	s, ok := domain.Sessions().Get(domain.SessionID(id))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("no such session"))
	}
	return s, ok
}

// handleSessionInput decodes every key BEFORE sending any, so a bad one in
// the batch refuses the whole batch rather than typing half of it.
func (s *Server) handleSessionInput(w http.ResponseWriter, r *http.Request) {
	var q sessionInputReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	ins := make([]domain.ConsoleInput, 0, len(q.Keys))
	for _, k := range q.Keys {
		in, err := domain.ConsoleKeyEvent(k)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ins = append(ins, in)
	}
	if sess.Info().State != domain.SessionRunning {
		writeErr(w, http.StatusConflict, errors.New("the session has exited"))
		return
	}
	for _, in := range ins {
		if in.IsKey {
			sess.SendKey(in.Key)
		} else {
			sess.SendText(in.Text)
		}
	}
	if q.Paste != "" {
		sess.Paste(q.Paste)
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSessionSize(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID         string `json:"id"`
		Cols, Rows int
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	cols, rows := domain.ClampConsoleSize(q.Cols, q.Rows)
	if err := sess.Resize(cols, rows); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]int{"cols": cols, "rows": rows})
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -run 'TestSessionInput|TestSessionSize' ./internal/web/ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Run the whole web package**

Run: `go test ./internal/web/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/web/console_input.go internal/web/console_input_test.go
git commit -m "feat(web): session input (keys, paste) and size endpoints"
```

---

### Task 6: `static/console.js` — painter, layer, input, size

**Files:**
- Create: `internal/web/static/console.js`
- Modify: `internal/web/static/app.js` (import), `internal/web/static/style.css` (console rules), `internal/web/static/keys.js` (nothing: the layer owns keys via `onKey`)
- Test: `internal/web/consolejs_test.go`

**Interfaces:**
- Consumes: Tasks 3–5's endpoints; `layers.js` (`pushLayer`, `closeLayer`, `pushFoot`, `popFoot`, `mountOverlay`, `topLayer`), `core.js` (`$`, `esc`, `elidePath`, `getJSON`, `postJSON`, `state`), `toast.js` (`toast`).
- Produces (exports): `openConsole(id)`, `closeConsole()`, `consoleSessionId()`, `consoleSessions(list)` (the live list changed: retitle / mark exited / close on gone).
  Pure section (`// --- console model (pure; guarded against Go) ---` … `// --- end console model ---`):
  ```js
  function keyToWire(e)            // KeyboardEvent-like {key, code, ctrlKey, altKey, shiftKey, metaKey} → {k, mod, text} | null (let through)
  function isReserved(e)           // ctrl+] or ctrl+\
  function gridSize(w, h, cw, ch)  // pixel box + cell size → {cols, rows}, each ≥ 1
  function runHTML(run)            // one run → <span …>text</span>
  function rowHTML(runs)           // a row → the spans, "" for blank
  function applyFrame(rows, frame) // rows: array of HTML strings; returns the new array (full replaces, partial patches by y)
  function consoleTitle(s, now)    // "claude · <wt> · running 12m" / "exited (3)" with the worktree cut by elide(path, n) passed in
  ```

- [ ] **Step 1: Write the failing JS-in-Go test**

```go
// internal/web/consolejs_test.go
package web

import (
	"strings"
	"testing"
)

const consolePureStart = "// --- console model (pure; guarded against Go) ---"
const consolePureEnd = "// --- end console model ---"

func TestConsoleModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "console.js", consolePureStart, consolePureEnd, `
const ev = (o) => Object.assign({ key: "", code: "", ctrlKey: false, altKey: false, shiftKey: false, metaKey: false }, o);
const r = [];
r.push(JSON.stringify(keyToWire(ev({ key: "a", code: "KeyA" }))));
r.push(JSON.stringify(keyToWire(ev({ key: "Enter", code: "Enter" }))));
r.push(JSON.stringify(keyToWire(ev({ key: "Tab", code: "Tab", shiftKey: true }))));
r.push(JSON.stringify(keyToWire(ev({ key: "c", code: "KeyC", ctrlKey: true }))));
r.push(JSON.stringify(keyToWire(ev({ key: "ArrowUp", code: "ArrowUp", ctrlKey: true, shiftKey: true }))));
r.push(JSON.stringify(keyToWire(ev({ key: "F5", code: "F5" }))));            // browser's: null
r.push(JSON.stringify(keyToWire(ev({ key: "w", code: "KeyW", ctrlKey: true }))));  // browser's: null
r.push(JSON.stringify(keyToWire(ev({ key: "T", code: "KeyT", ctrlKey: true, shiftKey: true }))));  // browser's: null
r.push(JSON.stringify(keyToWire(ev({ key: "Shift", code: "ShiftLeft" }))));  // a bare modifier: null
r.push(JSON.stringify(keyToWire(ev({ key: "Escape", code: "Escape" }))));
r.push(String(isReserved(ev({ key: "]", code: "BracketRight", ctrlKey: true }))), String(isReserved(ev({ key: "\\", code: "Backslash", ctrlKey: true }))), String(isReserved(ev({ key: "]", code: "BracketRight" }))));
r.push(JSON.stringify(gridSize(800, 400, 7.2, 16)), JSON.stringify(gridSize(0, 0, 7.2, 16)));
r.push(runHTML({ t: "a<b", fg: "#ff0000", b: true }), runHTML({ t: "x" }), rowHTML([]));
let rows = applyFrame([], { full: true, rows: 3, lines: [{ y: 0, runs: [{ t: "one" }] }, { y: 1, runs: [] }, { y: 2, runs: [{ t: "three" }] }] });
r.push(rows.length, rows[0], rows[1] === "", rows[2]);
rows = applyFrame(rows, { full: false, rows: 3, lines: [{ y: 1, runs: [{ t: "TWO" }] }] });
r.push(rows[0], rows[1], rows[2]);
r.push(consoleTitle({ label: "claude", worktree: "/a/b/wt", state: "running", started: new Date(Date.now() - 125000).toISOString() }, Date.now(), (p) => p));
r.push(consoleTitle({ label: "codex", worktree: "/a/b/wt", state: "exited", exit_code: 3 }, Date.now(), (p) => p));
console.log(r.join("|"));
`)
	want := `{"k":"char","mod":0,"text":"a"}|{"k":"enter","mod":0,"text":""}|{"k":"tab","mod":1,"text":""}|{"k":"char","mod":2,"text":"c"}|` +
		`{"k":"up","mod":3,"text":""}|null|null|null|null|{"k":"esc","mod":0,"text":""}|true|true|false|` +
		`{"cols":111,"rows":25}|{"cols":1,"rows":1}|` +
		`<span style="color:#ff0000" class="b">a&lt;b</span>|<span>x</span>||` +
		`3|<span>one</span>|true|<span>three</span>|<span>one</span>|<span>TWO</span>|<span>three</span>|` +
		`claude · /a/b/wt · running 2m|codex · /a/b/wt · exited (3)`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var consoleWiring = []struct{ file, want, why string }{
	{"app.js", "./console.js", "the module must be imported at boot"},
	{"console.js", `pushLayer("console"`, "the console rides the layer stack"},
	{"console.js", "/api/session-screen?id=", "frames come from the per-console stream"},
	{"console.js", "/api/session-input", "typing goes to the input endpoint"},
	{"console.js", "/api/session-size", "a focused console pushes its size"},
	{"console.js", `addEventListener("paste"`, "paste rides the paste event, not keys"},
	{"console.js", "stay with the browser", "the focused foot says which keys the browser keeps"},
	{"console.js", "ResizeObserver", "a focused console re-measures on resize"},
	{"style.css", "#console.hidden", "hidden by id, never a global .hidden"},
	{"style.css", "#console-grid", "the grid has its own rules (monospace, pre)"},
}

func TestConsoleJSIsWired(t *testing.T) {
	t.Parallel()
	for _, c := range consoleWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run 'TestConsole' ./internal/web/ 2>&1 | head -5`
Expected: FAIL — `static/console.js` missing (runPureJS fatals reading it) and the wiring lines absent.

- [ ] **Step 3: Write `console.js`**

```js
// console.js — an agent session's console in the page (web attach, plan 1):
// the server's screen painted as styled runs over a per-console SSE stream,
// keys and paste posted back, the focused viewer owning the session's size.
import { $, elidePath, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer, topLayer } from "./layers.js";
import { registerHelp } from "./menus.js";
import { toast } from "./toast.js";

// --- console model (pure; guarded against Go) ---
const KEY_NAMES = {
  Enter: "enter", Tab: "tab", Escape: "esc", Backspace: "backspace", Delete: "delete", Insert: "insert",
  ArrowUp: "up", ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right",
  Home: "home", End: "end", PageUp: "pgup", PageDown: "pgdn",
  F1: "f1", F2: "f2", F3: "f3", F4: "f4", F6: "f6", F7: "f7", F8: "f8", F9: "f9", F10: "f10",
};
// BROWSER_KEYS: combinations Chrome keeps for itself — never captured, never faked.
const BROWSER_CTRL = new Set(["w", "t", "n", "Tab"]);

function modOf(e) {
  return (e.shiftKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.altKey ? 4 : 0);
}

// keyToWire maps a keydown to the wire key, or null for a key the page must
// let through (the browser's own, a bare modifier, an unknown special key).
function keyToWire(e) {
  if (e.metaKey) return null;
  if (e.key === "F5" || e.key === "F11" || e.key === "F12") return null;
  if (e.ctrlKey && (BROWSER_CTRL.has(e.key) || (e.shiftKey && e.key.length === 1))) return null;
  const name = KEY_NAMES[e.key];
  if (name) return { k: name, mod: modOf(e), text: "" };
  if (e.key.length === 1 || [...e.key].length === 1) return { k: "char", mod: modOf(e), text: e.key };
  return null; // Shift, Control, CapsLock, Dead, Unidentified, …
}

function isReserved(e) {
  return e.ctrlKey && !e.altKey && !e.metaKey && (e.code === "BracketRight" || e.key === "]" || e.code === "Backslash" || e.key === "\\");
}

function gridSize(w, h, cw, ch) {
  return { cols: Math.max(1, Math.floor(w / cw)), rows: Math.max(1, Math.floor(h / ch)) };
}

function runHTML(run) {
  const st = [];
  if (run.fg) st.push("color:" + run.fg);
  if (run.bg) st.push("background:" + run.bg);
  const cls = ["b", "i", "u", "r", "d", "s"].filter((f) => run[f]).join(" ");
  return `<span${st.length ? ` style="${st.join(";")}"` : ""}${cls ? ` class="${cls}"` : ""}>${esc(run.t)}</span>`;
}

function rowHTML(runs) {
  return (runs || []).map(runHTML).join("");
}

// applyFrame folds a frame into the row array: a full frame replaces it,
// a partial one patches the rows it names.
function applyFrame(rows, frame) {
  const out = frame.full ? new Array(frame.rows).fill("") : rows.slice();
  if (frame.full) for (let i = 0; i < frame.rows; i++) out[i] = "";
  for (const l of frame.lines || []) out[l.y] = rowHTML(l.runs);
  return out;
}

function ageText(iso, now) {
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  if (s < 60) return s + "s";
  if (s < 3600) return Math.floor(s / 60) + "m";
  return Math.floor(s / 3600) + "h" + (Math.floor((s % 3600) / 60) || "");
}

function consoleTitle(s, now, elide) {
  const st = s.state === "exited" ? "exited (" + s.exit_code + ")" : "running " + ageText(s.started, now);
  return s.label + " · " + elide(s.worktree) + " · " + st;
}
// --- end console model ---
```

Then the live part (same file, below the marker):

```js
const con = { id: "", info: null, rows: [], frame: null, focused: false, max: false, es: null, queue: [], inflight: false, cell: null, size: null, tick: 0 };
const root = mountOverlay("console");
root.innerHTML =
  `<div id="console-title"><span id="console-label"></span><span id="console-size"></span></div>` +
  `<div id="console-body"><div id="console-grid"></div><div id="console-cursor" class="hidden"></div></div>`;
const grid = $("console-grid");
const cursor = $("console-cursor");

const FOOT_FOCUSED = `<button data-cact="out">ctrl+] step out</button><button data-cact="sessions">ctrl+\\ sessions</button><span class="cwarn">ctrl+w · ctrl+t · ctrl+n stay with the browser</span>`;
const FOOT_UNFOCUSED = `<button data-cact="focus">enter focus</button><button data-cact="max">m maximize</button><button data-cact="sessions">ctrl+\\ sessions</button><button data-cact="close">esc close</button>`;
const FOOT_EXITED = `<button data-cact="sessions">ctrl+\\ sessions</button><button data-cact="close">esc close</button>`;

$("foot").addEventListener("click", (e) => {
  const b = e.target.closest("button[data-cact]");
  if (!b || !con.id) return;
  ({ out: stepOut, focus: focusConsole, max: maximize, close: closeConsole, sessions: () => document.dispatchEvent(new CustomEvent("gg:switcher")) })[b.dataset.cact]();
});

function measureCell() {
  const probe = document.createElement("span");
  probe.textContent = "M".repeat(20);
  probe.style.visibility = "hidden";
  grid.append(probe);
  const r = probe.getBoundingClientRect();
  probe.remove();
  con.cell = { w: r.width / 20, h: r.height };
}

function layout() {
  const foot = $("foot").getBoundingClientRect();
  const panes = $("panes").getBoundingClientRect();
  const side = con.max ? null : $("branches-pane");
  const left = side && !side.classList.contains("hidden") && side.offsetWidth ? side.getBoundingClientRect().right + 5 : panes.left;
  Object.assign(root.style, { top: panes.top + "px", left: left + "px", right: "0", bottom: window.innerHeight - foot.top + "px" });
}

function paint() {
  grid.innerHTML = con.rows.map((r) => `<div class="crow">${r || " "}</div>`).join("");
  paintCursor();
}

function paintCursor() {
  const f = con.frame;
  if (!f || !f.cursor || con.info.state === "exited") return cursor.classList.add("hidden");
  cursor.classList.remove("hidden");
  cursor.classList.toggle("outline", !con.focused);
  cursor.style.left = f.cx * con.cell.w + "px";
  cursor.style.top = f.cy * con.cell.h + "px";
  cursor.style.width = con.cell.w + "px";
  cursor.style.height = con.cell.h + "px";
}

function retitle() {
  const budget = Math.max(12, Math.floor(($("console-title").clientWidth - 160) / (con.cell ? con.cell.w : 7)));
  $("console-label").textContent = consoleTitle(con.info, Date.now(), (p) => elidePath(p, budget));
  $("console-size").textContent = con.frame ? con.frame.cols + "×" + con.frame.rows : "";
  $("console-label").classList.toggle("exited", con.info.state === "exited");
}

function foot() {
  pushFoot("console", con.info.state === "exited" ? FOOT_EXITED : con.focused ? FOOT_FOCUSED : FOOT_UNFOCUSED);
}

function connect() {
  if (con.es) con.es.close();
  const es = new EventSource("/api/session-screen?id=" + encodeURIComponent(con.id));
  con.es = es;
  es.addEventListener("hello", (ev) => {
    const h = JSON.parse(ev.data);
    con.frame = h.frame;
    con.rows = applyFrame([], h.frame);
    paint();
    retitle();
  });
  es.addEventListener("frame", (ev) => {
    const f = JSON.parse(ev.data);
    con.frame = Object.assign({}, con.frame, f);
    con.rows = applyFrame(con.rows, f);
    paint();
    if (f.full) retitle();
  });
  es.addEventListener("exited", (ev) => {
    const wasFocused = con.focused;
    con.info = Object.assign({}, con.info, { state: "exited", exit_code: JSON.parse(ev.data).code });
    con.focused = false;
    retitle();
    foot();
    paintCursor();
    if (!wasFocused) toast(con.info.label + " in " + con.info.worktree.split("/").pop() + " exited (" + con.info.exit_code + ")");
  });
  es.addEventListener("gone", () => {
    toast(con.info.label + " was removed");
    closeConsole();
  });
}

async function pushSize() {
  if (!con.focused || !con.cell) return;
  const body = $("console-body").getBoundingClientRect();
  const sz = gridSize(body.width - 16, body.height - 12, con.cell.w, con.cell.h);
  if (con.size && con.size.cols === sz.cols && con.size.rows === sz.rows) return;
  con.size = sz;
  try {
    await postJSON("/api/session-size", { id: con.id, cols: sz.cols, rows: sz.rows });
  } catch (e) {
    toast("resize failed: " + (e.message || e), { err: true });
  }
}

const ro = new ResizeObserver(() => { layout(); if (con.focused) pushSize(); });

async function flush() {
  if (con.inflight || !con.queue.length) return;
  con.inflight = true;
  const batch = con.queue.splice(0);
  const keys = batch.filter((x) => x.k);
  const paste = batch.filter((x) => x.paste).map((x) => x.paste).join("");
  try {
    if (keys.length) await postJSON("/api/session-input", { id: con.id, keys });
    if (paste) await postJSON("/api/session-input", { id: con.id, paste });
  } catch (e) {
    if (e.status !== 409) toast("input failed: " + (e.message || e), { err: true });
  } finally {
    con.inflight = false;
    if (con.queue.length) flush();
  }
}

function send(item) {
  if (!con.info || con.info.state === "exited") return;
  con.queue.push(item);
  flush();
}

document.addEventListener("paste", (e) => {
  if (!con.id || !con.focused || topLayer().id !== "console") return;
  const text = (e.clipboardData || window.clipboardData).getData("text");
  if (!text) return;
  e.preventDefault();
  send({ paste: text });
});

function consoleKey(e) {
  if (isReserved(e)) {
    e.preventDefault();
    if (e.code === "Backslash" || e.key === "\\") document.dispatchEvent(new CustomEvent("gg:switcher"));
    else if (con.focused) stepOut();
    return true;
  }
  if (con.focused) {
    if (e.isComposing) return true; // the IME commits through keydown key === "Process" → ignored, then a char
    const k = keyToWire(e);
    if (!k) return e.key.length > 1 && !e.ctrlKey; // let the browser have its keys; swallow bare modifiers
    e.preventDefault();
    send(k);
    return true;
  }
  switch (e.key) {
    case "Enter": focusConsole(); break;
    case "m": maximize(); break;
    case "Escape": closeConsole(); break;
    default: return true; // an unfocused console still owns the keyboard: nothing leaks to the page
  }
  e.preventDefault();
  return true;
}

function focusConsole() {
  if (!con.info || con.info.state === "exited") return;
  con.focused = true;
  con.size = null;
  foot();
  paintCursor();
  pushSize();
}

function stepOut() {
  con.focused = false;
  if (con.max) { con.max = false; layout(); }
  foot();
  paintCursor();
}

function maximize() {
  if (!con.info || con.info.state === "exited") return;
  con.max = true;
  layout();
  focusConsole();
}

async function openConsole(id) {
  let body;
  try {
    body = await getJSON("/api/sessions");
  } catch (e) {
    return toast("sessions: " + (e.message || e), { err: true });
  }
  const info = (body.sessions || []).find((s) => s.id === id);
  if (!info) return toast("that agent session is gone", { err: true });
  if (con.id) closeConsole();
  Object.assign(con, { id, info, rows: [], frame: null, focused: false, max: false, queue: [], size: null });
  pushLayer("console", root, { onKey: consoleKey });
  layout();
  measureCell();
  ro.observe($("console-body"));
  connect();
  retitle();
  con.tick = setInterval(retitle, 15000);
  focusConsole();
}

function closeConsole() {
  if (!con.id) return;
  clearInterval(con.tick);
  ro.disconnect();
  if (con.es) con.es.close();
  con.es = null;
  con.id = "";
  con.focused = con.max = false;
  closeLayer("console");
  popFoot("console");
}

function consoleSessionId() {
  return con.id;
}

// consoleSessions: the live list changed — retitle the shown session, close on
// its removal (the stream's gone event also does; the list may land first).
function consoleSessions(list) {
  if (!con.id) return;
  const info = (list || []).find((s) => s.id === con.id);
  if (!info) return closeConsole();
  con.info = Object.assign({}, con.info, info);
  retitle();
}

grid.addEventListener("mousedown", () => { if (con.id && !con.focused) focusConsole(); });

registerHelp({
  key: "agent consoles",
  html:
    "<b>ctrl+\\</b> lists the agent sessions of this gg (Agents tab); <b>enter</b> opens one as a live console over the panes. " +
    "A focused console sends every key to the agent except <b>ctrl+]</b> (step out) and <b>ctrl+\\</b>; ctrl+w, ctrl+t and ctrl+n stay with the browser. " +
    "Unfocused: <b>enter</b> focus, <b>m</b> maximize, <b>esc</b> close (the session keeps running). The viewer that has the console focused sets its size.",
});

export { closeConsole, consoleSessionId, consoleSessions, openConsole };
```

`gg:switcher` is a document event the switcher (Task 7) listens to; until Task 7 lands it is inert. Add `import "./console.js";` to `app.js` after `./openfiles.js`. CSS (`style.css`, after the `#openfiles` block):

```css
/* The agent console (console.js): a layer over the panes right of the sidebar,
   the server's screen as styled runs. Hidden by id. */
#console { position: fixed; background: var(--bg-alt); display: flex; flex-direction: column; z-index: 20; border-left: 1px solid var(--border); }
#console.hidden { display: none; }
#console-title { padding: 6px 12px; border-bottom: 1px solid var(--border); white-space: nowrap; overflow: hidden; display: flex; }
#console-label.exited { color: var(--dim); }
#console-size { margin-left: auto; color: var(--dim); }
#console-body { flex: 1; overflow: auto; position: relative; padding: 6px 8px; }
#console-grid { font: 12px/16px ui-monospace, Menlo, Consolas, monospace; white-space: pre; display: inline-block; min-width: 100%; }
#console-grid .crow { height: 16px; }
#console-grid .b { font-weight: bold; } #console-grid .i { font-style: italic; } #console-grid .u { text-decoration: underline; }
#console-grid .d { opacity: .6; } #console-grid .s { text-decoration: line-through; }
#console-grid .r { filter: invert(1); }
#console-cursor { position: absolute; margin: 6px 0 0 8px; background: var(--fg); mix-blend-mode: difference; pointer-events: none; }
#console-cursor.outline { background: none; border: 1px solid var(--dim); mix-blend-mode: normal; }
#console-cursor.hidden { display: none; }
#foot .cwarn { margin-left: auto; color: #e0c06c; }
```

- [ ] **Step 4: Run the JS tests**

Run: `go test -run 'TestConsole' ./internal/web/ 2>&1 | tail -3`
Expected: PASS. If `applyFrame`'s full branch double-fills, simplify it to `new Array(frame.rows).fill("")` only.

- [ ] **Step 5: Run the whole web package**

Run: `go test ./internal/web/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/console.js internal/web/static/app.js internal/web/static/style.css internal/web/consolejs_test.go
git commit -m "feat(web): the agent console layer — painted frames, keys, paste, size ownership"
```

---

### Task 7: the tabbed `ctrl+\` switcher (Agents · AI tasks · Open files)

**Files:**
- Modify: `internal/web/static/openfiles.js` (becomes the tabbed switcher; keep the file name and exports), `internal/web/static/style.css`, `internal/web/static/index.html` (foot button label `ctrl+\ sessions · open files`)
- Test: `internal/web/switcherjs_test.go`

**Interfaces:**
- Consumes: Task 3's `/api/sessions`, `/api/tasks`; Task 6's `openConsole`, `consoleSessionId`.
- Produces: exports `isSwitcherKey`, `openSwitcher(tab?)`, `switcherOpenFiles(files)`, `switcherSessions(list)`; pure `sessionRows(list, mine)` → grouped rows `[{h: "repo"}|{h2: "wt — path"}|{id, glyph, label, wt, meta}]`, `taskRows(list)`, `freshestTab(sessions, tasks, files)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/web/switcherjs_test.go
package web

import (
	"strings"
	"testing"
)

func TestSwitcherModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", "// --- switcher model (pure; guarded against Go) ---", "// --- end switcher model ---", `
const r = [];
const sess = [
  { id: "s1", label: "claude", repo: "gg", worktree: "/x/gg", state: "running", started: new Date(Date.now() - 65000).toISOString() },
  { id: "s2", label: "codex", repo: "gg", worktree: "/x/wt2", state: "exited", exit_code: 0, started: new Date().toISOString() },
  { id: "s3", label: "Terminal", repo: "lazygit", worktree: "/y/lg", state: "running", started: new Date().toISOString(), task: "" },
  { id: "s4", label: "Claude · commit message", repo: "gg", worktree: "/x/gg", state: "running", started: new Date().toISOString(), task: "t1" },
];
const rows = sessionRows(sess, "s1", Date.now());
r.push(rows.map((x) => x.h ? "H:" + x.h : x.h2 ? "h2:" + x.h2 : x.id + ":" + x.glyph + ":" + x.mark + ":" + x.meta).join(","));
r.push(JSON.stringify(taskRows([{ id: "t1", key: "commit message — main @ abc", agent: "claude", state: "running", session: "s4", started: new Date().toISOString() }], Date.now())[0]));
r.push(freshestTab([{ started: "2026-01-01T00:00:00Z" }], [], [1]), freshestTab([], [], [1]), freshestTab([], [], []));
console.log(r.join("|"));
`)
	want := "H:gg,h2:gg — /x/gg,s1:●:●:1m,s4:●:○:0s,h2:wt2 — /x/wt2,s2:○:○:exited (0),H:lazygit,h2:lg — /y/lg,s3:●:○:0s|" +
		`{"id":"t1","key":"commit message — main @ abc","agent":"claude","state":"running","session":"s4","age":"0s"}|` +
		"agents|files|agents"
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var switcherWiring = []struct{ file, want, why string }{
	{"openfiles.js", `data-tab="agents"`, "the Agents tab exists"},
	{"openfiles.js", `data-tab="tasks"`, "the AI tasks tab exists"},
	{"openfiles.js", `data-tab="files"`, "the Open files tab exists"},
	{"openfiles.js", "/api/sessions", "the Agents tab lists the server's sessions"},
	{"openfiles.js", "/api/tasks", "the AI tasks tab lists the server's tasks"},
	{"openfiles.js", "openConsole(", "enter on a session opens its console"},
	{"openfiles.js", `"gg:switcher"`, "the console's ctrl+\\ reaches the switcher"},
	{"index.html", `ctrl+\ sessions`, "the foot advertises the switcher's new role"},
	{"live.js", "switcherSessions(", "a sessions event refreshes an open switcher"},
	{"live.js", "consoleSessions(", "a sessions event retitles the open console"},
}

func TestSwitcherJSIsWired(t *testing.T) {
	t.Parallel()
	for _, c := range switcherWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run 'TestSwitcher' ./internal/web/ 2>&1 | head -5`
Expected: FAIL — `sessionRows is not defined`.

- [ ] **Step 3: Implement**

In `openfiles.js`, extend the pure section:

```js
function ageOf(iso, now) {
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  return s < 60 ? s + "s" : s < 3600 ? Math.floor(s / 60) + "m" : Math.floor(s / 3600) + "h";
}

// sessionRows lays sessions out repo → worktree → session (the TUI's
// sessionsPopupRows). mine marks the session this tab's console shows.
function sessionRows(list, mine, now) {
  const byRepo = new Map();
  for (const s of list) {
    if (!byRepo.has(s.repo)) byRepo.set(s.repo, new Map());
    const dirs = byRepo.get(s.repo);
    if (!dirs.has(s.worktree)) dirs.set(s.worktree, []);
    dirs.get(s.worktree).push(s);
  }
  const rows = [];
  for (const repo of [...byRepo.keys()].sort()) {
    rows.push({ h: repo });
    const dirs = byRepo.get(repo);
    for (const wt of [...dirs.keys()].sort()) {
      rows.push({ h2: wt.split("/").pop() + " — " + wt });
      for (const s of dirs.get(wt)) {
        rows.push({
          id: s.id, label: s.label, wt, task: !!s.task,
          glyph: s.state === "exited" ? "○" : "●",
          mark: s.id === mine ? "●" : "○",
          meta: s.state === "exited" ? "exited (" + s.exit_code + ")" : ageOf(s.started, now),
          state: s.state,
        });
      }
    }
  }
  return rows;
}

function taskRows(list, now) {
  return list.map((t) => ({ id: t.id, key: t.key, agent: t.agent, state: t.state, session: t.session || "", age: ageOf(t.started || t.submitted, now) }));
}

// freshestTab: agents when any session exists, else files when any file is
// open, else agents (the empty Agents tab explains how to start one).
function freshestTab(sessions, tasks, files) {
  if (sessions.length) return "agents";
  if (files.length) return "files";
  return "agents";
}
```

Then rework the live part: `sw` gains `tab`, `sessions`, `tasks`, `rows` (the rendered rows of the active tab), `sel`; `openSwitcher(tab)` fetches `/api/sessions`, `/api/tasks`, `/api/open-files` in parallel (`Promise.all`, each `.catch(() => ({}))`), picks `tab || freshestTab(...)`, renders a tab strip `<div id="openfiles-tabs"><span data-tab="agents" class="on">Agents <i>N</i></span><span data-tab="tasks">AI tasks <i>N</i></span><span data-tab="files">Open files <i>N</i></span></div>` in `#openfiles-title`, and a list per tab: agents rows use `.ofrow` with `.grp` / `.grp2` for headers (not selectable; `clampSel` skips them — keep a `selectable` array of row indexes), tasks rows `key · agent · state · age`, files rows as today. Keys: `Tab` cycles tabs, `Enter` on agents → `closeSwitcher(); openConsole(id)`; on tasks with a `session` → `openConsole(session)`, without → nothing (plan 2 opens results); on files → `bringBack()`; `x` on files → `closeSelected()` (agents/tasks: plan 2); `/` filter on agents by label/worktree/repo (a small input like F's, or skip if the existing switcher has none — keep it a `sw.query` applied in `sessionRows`' caller); `Escape` closes; ctrl+\ toggles off. `document.addEventListener("gg:switcher", () => isOpen() ? closeSwitcher() : openSwitcher())`. Empty Agents tab text: `no agent sessions — start one from a worktree's menu in the TUI (the web starts them in plan 2)`.

`switcherSessions(list)`: when open on agents, replace `sw.sessions` and re-render keeping the selected id. `live.js`: on `msg.reason === "sessions"` call `switcherSessions(msg.sessions || [])` and `consoleSessions(msg.sessions || [])` (import both), then `return`.

`index.html` foot: change the `openfiles` button text to `ctrl+\ sessions · open files`. `keys.js`'s `case "openfiles"` stays (`openSwitcher()`).

CSS: `#openfiles-tabs { display:flex; gap:18px } #openfiles-tabs span { color: var(--dim); cursor: pointer } #openfiles-tabs span.on { color: var(--fg); border-bottom: 2px solid var(--accent) } #openfiles-tabs i { font-style: normal; font-size: 11px; margin-left: 4px } #openfiles-list .grp { color: var(--dim); font-size: 11px; text-transform: uppercase; padding: 3px 12px } #openfiles-list .grp2 { color: var(--dim); padding: 2px 20px } #openfiles-list .ofrow.task .glyph { color: #f2a65a } #openfiles-list .ofrow .glyph.run { color: #7bd88f }`.

- [ ] **Step 4: Run the tests**

Run: `go test -run 'TestSwitcher|TestOpenFiles|TestViewer|TestFinder' ./internal/web/ 2>&1 | tail -3`
Expected: PASS — the open-files switcher tests still hold (the `ofrow` class and `#openfiles-list` id are kept).

- [ ] **Step 5: Run the whole web package**

Run: `go test ./internal/web/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/openfiles.js internal/web/static/live.js internal/web/static/style.css internal/web/static/index.html internal/web/switcherjs_test.go
git commit -m "feat(web): ctrl+\\ is the tabbed switcher — Agents, AI tasks, Open files"
```

---

### Task 8: sidebar session sub-rows

**Files:**
- Modify: `internal/web/static/sidebar.js` (`renderWorktrees`, a `state.sessions` cache, click on a sub-row), `internal/web/static/live.js` (`sessions` → `takeSessions`), `internal/web/static/style.css`
- Test: `internal/web/sidebarsessions_test.go`

**Interfaces:**
- Consumes: Task 3's list; Task 6's `openConsole`.
- Produces: pure `worktreeSessionRows(sessions, path, now)` → `[{id, glyph, label, meta, task}]` for one worktree (this repo's sessions only: the served repo name is `state.repoName` — check `status.js`/`ops.js` for the field the page holds; sessions carry `repo`).

- [ ] **Step 1: Write the failing test**

```go
// internal/web/sidebarsessions_test.go
package web

import (
	"strings"
	"testing"
)

func TestWorktreeSessionRowsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "sidebar.js", "// --- sidebar model (pure; guarded against Go) ---", "// --- end sidebar model ---", `
const now = Date.now();
const sess = [
  { id: "s1", label: "claude", worktree: "/x/gg", state: "running", started: new Date(now - 125000).toISOString() },
  { id: "s2", label: "codex", worktree: "/x/other", state: "running", started: new Date(now).toISOString() },
  { id: "s3", label: "Claude · commit message — main @ abc", worktree: "/x/gg", state: "exited", exit_code: 2, started: new Date(now).toISOString(), task: "t1" },
];
console.log(JSON.stringify(worktreeSessionRows(sess, "/x/gg", now)));
`)
	want := `[{"id":"s1","glyph":"●","label":"claude","meta":"running 2m","task":false},{"id":"s3","glyph":"○","label":"Claude · commit message — main @ abc","meta":"exited (2)","task":true}]`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var sidebarSessionWiring = []struct{ file, want, why string }{
	{"sidebar.js", `class="wsess`, "session sub-rows render under their worktree"},
	{"sidebar.js", "openConsole(", "a click on a sub-row opens the console"},
	{"sidebar.js", "takeSessions", "the sessions event refreshes the rows"},
	{"live.js", "takeSessions(", "live.js forwards the sessions list to the sidebar"},
	{"style.css", ".wsess", "sub-rows are styled"},
}

func TestSidebarSessionsWired(t *testing.T) {
	t.Parallel()
	for _, c := range sidebarSessionWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
```

If `sidebar.js` has no pure-section markers yet, add them around the new function (the first guarded helper in that file).

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run 'TestWorktreeSessionRows|TestSidebarSessions' ./internal/web/ 2>&1 | head -5`
Expected: FAIL — markers missing / `worktreeSessionRows` undefined.

- [ ] **Step 3: Implement**

```js
// --- sidebar model (pure; guarded against Go) ---
function sessAge(iso, now) {
  const s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  return s < 60 ? s + "s" : s < 3600 ? Math.floor(s / 60) + "m" : Math.floor(s / 3600) + "h";
}

// worktreeSessionRows: the sessions running in one worktree, in start order —
// the TUI's Worktrees sub-rows (└ ● claude  running 12m).
function worktreeSessionRows(sessions, path, now) {
  return sessions
    .filter((s) => s.worktree === path)
    .map((s) => ({
      id: s.id,
      glyph: s.state === "exited" ? "○" : "●",
      label: s.label,
      meta: s.state === "exited" ? "exited (" + s.exit_code + ")" : "running " + sessAge(s.started, now),
      task: !!s.task,
    }));
}
// --- end sidebar model ---
```

`state.sessions = []` (declare in `core.js`'s `state` if it is a plain object literal; else on `state` at load). `takeSessions(list)` sets it and calls `renderWorktrees()`; `renderWorktrees` appends after each `<li>` one `<li class="wsess${r.task ? " task" : ""}" data-sid="${esc(r.id)}" title="${esc(r.label)}">└ <span class="glyph ${r.glyph === "●" ? "run" : "ex"}">${r.glyph}</span> ${esc(r.label)} <span class="wpath">${esc(r.meta)}</span></li>` (label cut with `elideNameMiddle` to the row budget). Click handler on `#worktrees-list`: a `li.wsess` → `openConsole(li.dataset.sid)`. The right-click menu on a sub-row: `Open` only in this plan (plan 2 adds Kill/Remove). At boot, `fetchSidebar`/`loadRepo` also fetches `/api/sessions` once (`getJSON("/api/sessions").catch(() => ({ sessions: [] }))`) so the rows exist before any event. `live.js`: the `sessions` branch also calls `takeSessions(msg.sessions || [])`.

CSS: `#worktrees-list li.wsess { padding-left: 22px; cursor: pointer; color: var(--fg); font-weight: normal } #worktrees-list li.wsess .glyph.run { color: #7bd88f } #worktrees-list li.wsess .glyph.ex { color: var(--dim) } #worktrees-list li.wsess.task .glyph.run { color: #f2a65a }`.

Note the existing `contextmenu` handler on `#worktrees-list` reads `li.dataset.p`; a sub-row has none, so it is ignored there (right).

- [ ] **Step 4: Run the tests**

Run: `go test -run 'TestWorktreeSessionRows|TestSidebarSessions|TestSidebar' ./internal/web/ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Run the whole web package**

Run: `go test ./internal/web/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/web/static/sidebar.js internal/web/static/live.js internal/web/static/core.js internal/web/static/style.css internal/web/sidebarsessions_test.go
git commit -m "feat(web): session sub-rows under worktrees in the sidebar"
```

---

### Task 9: TUI size ownership — no push while unfocused on resize, push on focus gain

**Files:**
- Modify: `internal/tui/console.go` (`syncConsoleSize` callers), `internal/tui/model.go:529`
- Test: `internal/tui/console_test.go`

**Interfaces:**
- Consumes: nothing new. Produces: `func (m Model) syncConsoleSizeIfFocused() Model` — the window-resize path uses it; `openConsole`, `enter` (focus) and `ctrl+t` (maximize) keep calling `syncConsoleSize`.

- [ ] **Step 1: Write the failing tests**

```go
func TestUnfocusedConsoleDoesNotPushItsSizeOnWindowResize(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	m.console.focused = false
	_ = s.Resize(100, 30) // another viewer (the web) owns the size now
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	m = mm.(Model)
	if sc := s.Screen(); sc.Cols != 100 || sc.Rows != 30 {
		t.Fatalf("an unfocused console pushed %dx%d on window resize", sc.Cols, sc.Rows)
	}
}

func TestConsolePushesItsSizeWhenItGainsFocus(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	m.console.focused = false
	_ = s.Resize(100, 30)
	m.focus = panelCommits
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if sc := s.Screen(); !m.console.focused || sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("after focus: focused=%v emulator %dx%d, want %dx%d", m.console.focused, sc.Cols, sc.Rows, cols, rows)
	}
}
```

Check `keyMsg` exists in the tui tests (grep `func keyMsg`); otherwise build `tea.KeyMsg{Type: tea.KeyEnter}` directly.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd internal/tui && go test -run 'TestUnfocusedConsoleDoesNotPush|TestConsolePushesItsSizeWhenItGainsFocus' ./ 2>&1 | tail -4`
Expected: both FAIL (the first: the resize pushed; the second: focus did not push).

- [ ] **Step 3: Implement**

In `console.go`, after `syncConsoleSize`:

```go
// syncConsoleSizeIfFocused is the window-resize rule: only a focused (or
// maximised) console owns the session's size. An unfocused one follows
// whatever viewer is typing — the web page, or another gg — and clips.
func (m Model) syncConsoleSizeIfFocused() Model {
	if m.console == nil || !m.console.focused {
		return m
	}
	return m.syncConsoleSize()
}
```

`model.go:529` → `m = m.syncConsoleSizeIfFocused()`. In the unfocused `case "enter":` branch of the console key handler: `m.console.focused = true; return m.syncConsoleSize(), nil, true`. Keep every other `syncConsoleSize` call (open, maximize, un-maximize by ctrl+], the exit step-out) as it is: those are the user's own actions at the TUI. `TestConsoleFollowsBoxSize` resizes a *focused* console (openConsole focuses), so it still passes.

- [ ] **Step 4: Run the console tests**

Run: `cd internal/tui && go test -run 'Console' ./ 2>&1 | tail -3`
Expected: PASS.

- [ ] **Step 5: Run the whole tui package**

Run: `go test ./internal/tui/ 2>&1 | tail -2`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/console.go internal/tui/model.go internal/tui/console_test.go
git commit -m "feat(tui): a console owns the session size only while focused; focus gain pushes it"
```

---

### Task 10: browser check, `docs/CLAUDE-details.md`, CHANGELOG, README, memory

**Files:**
- Create (scratchpad, not committed): `runweb.sh`, `checkattach.mjs`
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`
- Memory: `agent-sessions-feature.md`

- [ ] **Step 1: Build the verify binary and start it in isolation**

```bash
cd <worktree> && go build -o /tmp/gg-attach ./cmd/gg
```

In plan 1 the web cannot start a session, and a `gg web` process has no headless way to host one, so the check runs against the test server instead of the installed binary: `internal/web/attach_browser_test.go`, guarded by `GG_BROWSER_CHECK=1` (skipped otherwise), starts a real `sh -c 'while read l; do eval "$l"; done'` session under a private manager, serves `New(domain.Open(repo))` on a loopback listener with `XDG_STATE_HOME`/`XDG_CONFIG_HOME` isolated, prints the URL, and blocks until the file named by `GG_BROWSER_DONE` appears. `runweb.sh` (scratchpad) runs that test in the background, reads the URL from its output, runs `checkattach.mjs` against it, then touches the done file. A real page, a real session, a real stream — only the start path is borrowed. The **unfixed** comparison uses the installed `gg web` (port 0, URL from its log, own PID killed after): its switcher has no Agents tab, so steps 1–2 fail there by construction; record that run's output in the ledger before the fixed one.

- [ ] **Step 2: `checkattach.mjs` asserts, in order** (each with visibility via `getBoundingClientRect` + computed style as the earlier `vis()` helper):

  1. load → `ctrl+\` → `#openfiles` visible, tab strip shows `Agents 1`, a row with `sh`;
  2. `Enter` → `#console` visible, `#console-grid` contains the shell prompt (`$`), the foot contains `ctrl+] step out`;
  3. type `echo hello-attach` + `Enter` → within 3 s `#console-grid` contains `hello-attach` twice (echo + output);
  4. `ctrl+]` → foot shows `enter focus`, `#console-cursor` has class `outline`;
  5. `m` → `#console` left edge is 0 (sidebar hidden), foot back to focused;
  6. a second page → `ctrl+\` → `Enter` → its grid shows `hello-attach` too (same screen);
  7. in page 2, focus and type `exit` + `Enter` → page 1's title contains `exited (0)` and its foot `esc close`;
  8. `Escape` twice (step out is automatic on exit; esc closes) → `#console` hidden.

  Run it against the **unfixed** installed build first: `gg web` v0.3.0-355 has no `/api/sessions`, so steps 1–2 must FAIL there (the switcher shows only open files). Record both outputs in the ledger.

- [ ] **Step 3: Docs**

CHANGELOG (top of Unreleased): "**Web attach, plan 1:** `gg web` shows the agent sessions of this process — `ctrl+\` is now a tabbed switcher (Agents · AI tasks · Open files), a session opens as a live console over the panes (the server's screen painted as styled runs over its own SSE stream), typing and paste go to the agent, the focused viewer owns the session's size; session sub-rows under worktrees in the sidebar. TUI: an unfocused console no longer pushes its size on window resize; gaining focus pushes it."

README: in the web section, one paragraph on the console and the switcher, keys listed; note that starting sessions from the web is the next plan.

`docs/CLAUDE-details.md`: a `### Web attach` subsection: the frame protocol (hello/frame/exited/gone, full vs partial rows, 40 ms coalesce, skipped streams get a full frame), the key wire and the shared table rule, the size-ownership rule on both frontends, the `sessions` hub event bypassing the op gate, and the gotcha: never select on `domain.Sessions().Changed()` from two places (one coalesced channel).

Memory `agent-sessions-feature.md`: append "WEB ATTACH plan 1 (branch feat/web-attach): …" with the rulings above and the spec/plan paths.

- [ ] **Step 4: Full gates**

Run: `./test.sh 2>&1 | tail -5` then `./test.sh race 2>&1 | tail -5`
Expected: both green. Never edit the tree while they run.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md docs/CLAUDE-details.md internal/web/attach_browser_test.go
git commit -m "docs: web attach plan 1 — console protocol, keys, size ownership"
```

Then ask before merging (`merge --no-ff` with a custom message; keep both CHANGELOG sides on conflict), `go build ./cmd/gg && go test ./internal/web/` on the merged tree, `./build.sh install && ./build.sh web`, remove the worktree and branch.
