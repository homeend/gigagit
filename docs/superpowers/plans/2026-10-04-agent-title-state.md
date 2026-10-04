# Agent Title & Progress State Signal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Running agents read as working for a whole turn and turn idle ~0.7 s after it ends, using the terminal title (Claude, Codex) and OSC 9;4 progress (Kimi) as a signal beside the screen.

**Architecture:** `agentsession.oscFilter` records the last OSC 0/2 title and OSC 9;4 state from the raw bytes (before it strips non-ASCII for x/vt); `Session.Signals()` exposes them. `agentstate` gains built-in title rules and `ClassifyWith` (herdr's order: title question → title working → screen → title idle). `domain.StateWatcher` reads signals with the screen, keeps a per-session "has animated" guard, holds a titled idle 700 ms (2 s otherwise) and records the hold in `SessionActivity.Settle`, which `agent_wait` honours.

**Tech Stack:** Go 1.26, `x/vt` emulator, stdlib `regexp`.

**Spec:** `docs/superpowers/specs/2026-10-04-agent-title-state-design.md`

## Global Constraints

- Built-in title rules only — no config lists; `screen_*` config replaces only the screen lists.
- Title capture cap: 256 bytes, cut to valid UTF-8. Only OSC 0, 2 and 9 payloads are ever buffered.
- Bytes handed to the emulator are byte-identical to today's filter output.
- Titled idle hold: 700 ms (`titleSettle`); untitled: `idleSettle` 2 s (unchanged).
- Stall rules (`stallAfter` 2 min, `spinStallAfter` 10 min) unchanged.
- Surfaces (rows, notices, web, `gg agent list`, `agent_wait` events) unchanged.
- Test-first; `gofmt -w` before every commit; commits via `gg add <files>` + `git commit -F <msgfile>`; messages end with the two attribution lines (Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com> / Claude-Session: https://claude.ai/code/session_01VGLjRP4v2My6Wa2d9xXfGz).
- New tests call `t.Parallel()` unless they swap process globals.

## Review Focus

1. A title split across two PTY reads, or terminated by ESC `\` rather than BEL, must be captured whole — and an OSC 52 clipboard payload of megabytes must never be buffered.
2. A Claude permission dialog during a turn: the title turns "✳" while the screen shows the dialog → question, never idle (Task 3 test `dialog under ✳`).
3. A Claude whose title never animates (static "✳", e.g. an unstripped multiplexer) must keep today's 2 s hold — the guard (Task 5 test `NeverAnimatedKeeps2s`).
4. A session whose title says working while the screen shows the empty prompt between two steps must not post an idle notice (Task 5 test `TitleWorkingBridgesTheGap`).
5. `agent_wait` on a titled idle wakes after ~700 ms, and still waits 2 s on an untitled one (Task 5 agent_wait tests).

---

### Task 1: oscFilter records the title and the progress state

**Files:**
- Modify: `internal/agentsession/oscfilter.go`
- Test: `internal/agentsession/oscfilter_test.go`

**Interfaces:**
- Produces: on `oscFilter` — fields read by Task 2: `title string`, `progress int`, `hasProgress bool`, `changed bool` (set when either value was committed; Task 2 clears it). Constant `titleCap = 256`.

- [ ] **Step 1: Write the failing tests** (append to `oscfilter_test.go`; add `"bytes"` to imports)

```go
// Captured live 2026-10-04 (script, env as gg gives it): Claude Code 2.1.289,
// Codex 0.160.0, Kimi Code 2.1.1.
var (
	capClaudeIdle    = []byte("\x1b]0;\xe2\x9c\xb3 Claude Code\x07")                      // "✳ Claude Code"
	capClaudeWorking = []byte("\x1b]0;\xe2\x97\x90 Math and shell command sequence\x07") // "◐ …"
	capCodexWorking  = []byte("\x1b]0;\xe2\xa0\x8b repo\x07")                            // "⠋ repo"
	capCodexIdle     = []byte("\x1b]0;Run sleep 5 command | repo\x07")
	capKimiBusy      = []byte("\x1b]9;4;3\x07")
	capKimiClear     = []byte("\x1b]9;4;0\x07")
)

func TestOSCFilterRecordsTheTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []byte
		want string
	}{
		{capClaudeIdle, "✳ Claude Code"},
		{capClaudeWorking, "◐ Math and shell command sequence"},
		{capCodexWorking, "⠋ repo"},
		{capCodexIdle, "Run sleep 5 command | repo"},
		{[]byte("\x1b]2;◑ x\x1b\\"), "◑ x"}, // OSC 2, ST-terminated
		{[]byte("\x1b]0;\x07"), ""},          // Claude clears it on exit
	}
	for _, c := range cases {
		var f oscFilter
		f.filter(bytes.Join([][]byte{[]byte("A"), c.in, []byte("B")}, nil)) // never append to a shared fixture
		if f.title != c.want || !f.changed {
			t.Errorf("%q: title = %q changed=%v, want %q", c.in, f.title, f.changed, c.want)
		}
	}
}

func TestOSCFilterRecordsTheTitleAcrossReads(t *testing.T) {
	t.Parallel()
	in := append(append([]byte("X"), capClaudeWorking...), "AB"...)
	for cut := 1; cut < len(in); cut++ {
		var f oscFilter
		f.filter(in[:cut])
		f.filter(in[cut:])
		if f.title != "◐ Math and shell command sequence" {
			t.Fatalf("cut at %d: title = %q", cut, f.title)
		}
	}
}

func TestOSCFilterRecordsProgress(t *testing.T) {
	t.Parallel()
	var f oscFilter
	if f.hasProgress {
		t.Fatal("progress before any OSC 9;4")
	}
	f.filter(capKimiBusy)
	if !f.hasProgress || f.progress != 3 {
		t.Fatalf("busy: %v %d", f.hasProgress, f.progress)
	}
	f.filter([]byte("\x1b]9;4;1;40\x07")) // state;value
	if f.progress != 1 {
		t.Fatalf("normal with a value: %d", f.progress)
	}
	f.filter(capKimiClear)
	if f.progress != 0 {
		t.Fatalf("clear: %d", f.progress)
	}
	f.filter([]byte("\x1b]9;hello\x07\x1b]9;4;9\x07")) // a plain OSC 9 notification, an unknown state
	if f.progress != 0 {
		t.Fatalf("non-progress OSC 9 changed it: %d", f.progress)
	}
}

func TestOSCFilterIgnoresOtherAndCancelledStrings(t *testing.T) {
	t.Parallel()
	var f oscFilter
	f.filter(capClaudeIdle)
	f.changed = false
	big := bytes.Repeat([]byte("QUJD"), 1<<18) // 1 MiB of base64
	f.filter(append(append([]byte("\x1b]52;c;"), big...), 0x07))
	f.filter([]byte("\x1b]8;;https://x\x07link\x1b]8;;\x07"))
	f.filter([]byte("\x1b]0;◐ cancelled\x18"))  // CAN
	f.filter([]byte("\x1b]0;◐ broken\x1b[0m")) // ESC that is not ST
	if f.title != "✳ Claude Code" || f.changed {
		t.Fatalf("title = %q changed=%v", f.title, f.changed)
	}
	if cap(f.pay) > 2*titleCap {
		t.Fatalf("buffered %d bytes", cap(f.pay))
	}
}

func TestOSCFilterCapsTheTitleOnARune(t *testing.T) {
	t.Parallel()
	var f oscFilter
	long := strings.Repeat("◐", 200) // 600 bytes
	f.filter([]byte("\x1b]0;" + long + "\x07"))
	if len(f.title) > titleCap || !utf8.ValidString(f.title) || !strings.HasPrefix(long, f.title) || len(f.title) < titleCap-3 {
		t.Fatalf("capped title: %d bytes valid=%v", len(f.title), utf8.ValidString(f.title))
	}
}

// The emulator sees exactly what it saw before the recording existed.
func TestOSCFilterOutputUnchangedByRecording(t *testing.T) {
	t.Parallel()
	in := bytes.Join([][]byte{capClaudeIdle, []byte("é\x1b[1m"), capKimiBusy, capCodexWorking, []byte("\x1b]0;◐ x\x1b\\tail")}, nil)
	var f oscFilter
	got := string(f.filter(in))
	want := "\x1b]0; Claude Code\x07é\x1b[1m\x1b]9;4;3\x07\x1b]0; repo\x07\x1b]0; x\x1b\\tail"
	if got != want {
		t.Fatalf("filter output\n got %q\nwant %q", got, want)
	}
}
```

Add `"unicode/utf8"` and `"bytes"` to the test imports.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/agentsession/ -run 'OSCFilter' -count=1`
Expected: compile FAIL — `f.title undefined`, `titleCap undefined` (the existing OSCFilter tests would pass).

- [ ] **Step 3: Implement** — replace `oscfilter.go`'s type and `filter` with:

```go
// oscFilter strips non-ASCII bytes from the payload of string escape
// sequences (OSC, DCS, APC, PM, SOS) before they reach the emulator.
//
// x/ansi's parser treats byte 0x9C as the C1 String Terminator inside those
// strings even when it is the middle of a UTF-8 character, so a window title
// like "✳ Claude Code" (E2 9C B3 …) ends early and the rest is printed at the
// cursor — Claude Code sets such titles on every spinner frame. The payloads
// are titles, hyperlink targets and base64 clipboard data, none of which the
// console shows, so dropping their non-ASCII bytes costs nothing visible;
// UTF-8 in ordinary screen text is untouched. State carries across calls, so
// a sequence split between two reads is filtered the same.
//
// It also records, from the unfiltered bytes, the last window title (OSC 0
// or 2) and the last OSC 9;4 progress state: agents announce working and
// idle there (agentstate's title rules). Only those payloads are buffered,
// the title capped at titleCap.
type oscFilter struct {
	state filterState
	out   []byte

	osc     oscKind // the OSC being read: unknown until its number ends
	num     []byte  // the OSC number read so far
	pay     []byte  // a title/progress payload read so far
	title   string  // the last complete title
	progress    int  // the last OSC 9;4 state (0–4)
	hasProgress bool // an OSC 9;4 has been seen
	changed     bool // title or progress committed since the reader last cleared it
}

type filterState uint8

const (
	fGround    filterState = iota
	fEsc                   // saw ESC in ground
	fString                // inside an OSC/DCS/APC/PM/SOS payload
	fStringEsc             // saw ESC inside a payload (ESC \ ends it)
)

type oscKind uint8

const (
	oscNone     oscKind = iota // not an OSC, or one gg does not record
	oscNumber                  // an OSC whose number is still being read
	oscTitle                   // OSC 0 / OSC 2
	oscProgress                // OSC 9 (a progress report only if it reads "4;<state>")
)

// titleCap bounds a recorded title; longer payloads are cut on a rune.
const titleCap = 256

// filter returns p with string-payload bytes >= 0x80 removed. The returned
// slice is reused by the next call.
func (f *oscFilter) filter(p []byte) []byte {
	f.out = f.out[:0]
	for _, b := range p {
		switch f.state {
		case fGround:
			if b == 0x1B {
				f.state = fEsc
			}
		case fEsc:
			switch b {
			case ']':
				f.state, f.osc, f.num, f.pay = fString, oscNumber, f.num[:0], f.pay[:0]
			case 'P', '_', '^', 'X': // DCS, APC, PM, SOS
				f.state, f.osc = fString, oscNone
			case 0x1B:
				// ESC ESC: still at an escape
			default:
				f.state = fGround
			}
		case fString:
			switch {
			case b == 0x07: // BEL ends
				f.commit()
				f.state = fGround
			case b == 0x18, b == 0x1A: // CAN/SUB cancel
				f.osc, f.state = oscNone, fGround
			case b == 0x1B:
				f.state = fStringEsc
			default:
				f.record(b)
				if b >= 0x80 {
					continue // the byte x/ansi could mistake for a C1 terminator
				}
			}
		case fStringEsc:
			switch b {
			case '\\': // ST
				f.commit()
				f.state = fGround
			case ']':
				f.state, f.osc, f.num, f.pay = fString, oscNumber, f.num[:0], f.pay[:0]
			case 'P', '_', '^', 'X': // ESC ends the string and opens a new one
				f.state, f.osc = fString, oscNone
			case 0x1B:
				f.state, f.osc = fEsc, oscNone
			default:
				f.state, f.osc = fGround, oscNone
			}
		}
		f.out = append(f.out, b)
	}
	return f.out
}

// record takes one payload byte of the current string.
func (f *oscFilter) record(b byte) {
	switch f.osc {
	case oscNumber:
		if b == ';' {
			switch string(f.num) {
			case "0", "2":
				f.osc = oscTitle
			case "9":
				f.osc = oscProgress
			default:
				f.osc = oscNone
			}
			return
		}
		if b < '0' || b > '9' || len(f.num) >= 4 {
			f.osc = oscNone
			return
		}
		f.num = append(f.num, b)
	case oscTitle:
		if len(f.pay) < titleCap+utf8.UTFMax {
			f.pay = append(f.pay, b)
		}
	case oscProgress:
		if len(f.pay) < 16 {
			f.pay = append(f.pay, b)
		}
	}
}

// commit stores a complete title or progress report.
func (f *oscFilter) commit() {
	switch f.osc {
	case oscTitle:
		t := f.pay
		if len(t) > titleCap {
			t = t[:titleCap]
		}
		f.title, f.changed = strings.ToValidUTF8(string(t), ""), true
	case oscNumber: // "ESC ] 0 BEL": a number with no payload
		if n := string(f.num); n == "0" || n == "2" {
			f.title, f.changed = "", true
		}
	case oscProgress:
		rest, ok := strings.CutPrefix(string(f.pay), "4;")
		if !ok {
			break
		}
		st, _, _ := strings.Cut(rest, ";")
		if len(st) == 1 && st[0] >= '0' && st[0] <= '4' {
			f.progress, f.hasProgress, f.changed = int(st[0]-'0'), true, true
		}
	}
	f.osc = oscNone
}
```

Add `import ("strings"; "unicode/utf8")` to `oscfilter.go`.

Note: `\x1b]0;\x07` reaches `commit` in state `oscTitle` (the `;` switched it) with an empty payload → title "" — covered by the `""` case. The `oscNumber` branch covers `ESC ] 0 BEL` without a semicolon.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agentsession/ -run 'OSCFilter' -count=1`
Expected: PASS (all old and new OSCFilter tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/agentsession/oscfilter.go internal/agentsession/oscfilter_test.go
gg add internal/agentsession/oscfilter.go internal/agentsession/oscfilter_test.go
git commit -F <msgfile>   # "feat(agentsession): record the title and OSC 9;4 progress"
```

---

### Task 2: Session.Signals() and a multiplexer-free child env

**Files:**
- Modify: `internal/agentsession/session.go` (struct, `pumpOut`, `childEnv`), `internal/agentsession/types.go` (the `Signals` type)
- Test: `internal/agentsession/session_test.go`, `internal/agentsession/childenv_test.go`

**Interfaces:**
- Consumes: Task 1's `oscFilter.title/progress/hasProgress/changed`.
- Produces: `type Signals struct { Title string; Progress int }` (Progress −1 = none) and `func (s *Session) Signals() Signals`.

- [ ] **Step 1: Write the failing tests**

`childenv_test.go` — extend the existing test's input and expectation:

```go
func TestChildEnvDropsTheHostsAgentChannel(t *testing.T) {
	got := childEnv([]string{"PATH=/bin", "GG_SESSION_ID=p/s1", "GG_MCP_URL=http://x", "GG_SESSION_TOKEN=abc", "GG_PARENT_SESSION=p/s0", "TMUX=1", "GG_INBOX=/i",
		"STY=1.pts-0", "ZELLIJ=0", "ZELLIJ_SESSION_NAME=z", "ZELLIJ_PANE_ID=3"})
	if !slices.Equal(got, []string{"PATH=/bin", "GG_INBOX=/i"}) {
		t.Fatalf("childEnv = %v", got)
	}
}
```

`session_test.go`:

```go
// The agent's title and progress reach Signals with their UTF-8 intact.
func TestSessionSignals(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `printf 'a\033]0;\342\227\220 Claude Code\007b'; sleep 5`)
	if got := s.Signals(); got.Progress != -1 {
		t.Fatalf("before any report: %+v", got)
	}
	eventually(t, "title", func() bool { return s.Signals().Title == "◐ Claude Code" })
	if got := s.Signals(); got.Progress != -1 {
		t.Fatalf("progress = %d", got.Progress)
	}
}

func TestSessionSignalsProgress(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `printf '\033]9;4;3\007'; sleep 5`)
	eventually(t, "progress", func() bool { return s.Signals().Progress == 3 })
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/agentsession/ -run 'ChildEnv|SessionSignals' -count=1`
Expected: compile FAIL — `s.Signals undefined`.

- [ ] **Step 3: Implement**

`types.go`:

```go
// Signals is what the agent announces outside the screen: the last window
// title (OSC 0/2) and the last OSC 9;4 progress state (0 clear, 1 normal,
// 2 error, 3 busy, 4 paused; −1 before any).
type Signals struct {
	Title    string
	Progress int
}
```

`session.go` struct — next to `osc`:

```go
	sigMu sync.Mutex
	sig   Signals // the title/progress oscFilter recorded; under sigMu
```

In `start`, where the Session literal is built (before `go s.pumpOut()`), set `s.sig.Progress = -1`.

`pumpOut` — replace the emulator write line:

```go
			s.withEmu(func() { _, _ = s.emu.Write(s.osc.filter(buf[:n])) })
			if s.osc.changed {
				s.osc.changed = false
				s.sigMu.Lock()
				s.sig.Title = s.osc.title
				if s.osc.hasProgress {
					s.sig.Progress = s.osc.progress
				}
				s.sigMu.Unlock()
			}
```

New method (after `LastOutput`):

```go
// Signals is the agent's last title and progress report.
func (s *Session) Signals() Signals {
	s.sigMu.Lock()
	defer s.sigMu.Unlock()
	return s.sig
}
```

`childEnv` — doc and switch:

```go
// childEnv drops the variables that describe gg's own host terminal rather
// than the console the child runs in: an agent seeing TMUX assumes it is a
// tmux pane (Claude prints tmux scroll hints), and under any multiplexer
// (TMUX, STY, ZELLIJ*) Claude freezes its title at "✳", the working signal
// the state watcher reads. TERM is set explicitly. A gg started inside a gg
// console must not hand ITS agent-channel identity (GG_SESSION_ID,
// GG_MCP_URL, GG_SESSION_TOKEN, GG_PARENT_SESSION) on.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "TMUX", k == "TMUX_PANE", k == "STY", k == "TERM",
			k == "GG_SESSION_ID", k == "GG_MCP_URL", k == "GG_SESSION_TOKEN", k == "GG_PARENT_SESSION",
			strings.HasPrefix(k, "ZELLIJ"):
			continue
		}
		out = append(out, kv)
	}
	return out
}
```

(If the Session is constructed somewhere the `-1` initialisation cannot reach — check `start` — initialise in the literal: `sig: Signals{Progress: -1}`.)

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agentsession/ -count=1 -race`
Expected: PASS.

- [ ] **Step 5: Commit** — `gofmt -w` the four files; `gg add` them; message `feat(agentsession): Session.Signals; drop STY/ZELLIJ from the agent env`.

---

### Task 3: agentstate title rules and ClassifyWith

**Files:**
- Modify: `internal/agentstate/classify.go`
- Test: `internal/agentstate/classify_test.go` (append; check the file name with `ls internal/agentstate`)

**Interfaces:**
- Produces:
  - `Rules` fields `TitleWorking, TitleQuestion, TitleIdle []*regexp.Regexp; ProgressBusy bool`
  - `type Signal struct { Title string; Progress int; Trusted bool }` (Progress −1 = none)
  - `func SignalState(r Rules, sig Signal) State` — what title/progress alone say (Trusted ignored)
  - `func ClassifyWith(r Rules, lines []string, sig Signal) (st State, titleIdle bool)`
  - `DefaultRules` fills the title fields for claude, codex, kimi.

- [ ] **Step 1: Write the failing tests**

```go
func sig(title string) Signal { return Signal{Title: title, Progress: -1, Trusted: true} }

// Captured live 2026-10-04 (Claude Code 2.1.289, Codex 0.160.0, Kimi 2.1.1).
func TestSignalState(t *testing.T) {
	t.Parallel()
	claude, codex, kimi, junie := DefaultRules("claude"), DefaultRules("codex"), DefaultRules("kimi"), DefaultRules("junie")
	cases := []struct {
		name string
		r    Rules
		s    Signal
		want State
	}{
		{"claude ◐", claude, sig("◐ Math and shell command sequence"), Working},
		{"claude ◑", claude, sig("◑ Claude Code"), Working},
		{"claude ◓ (2.1.228+ set)", claude, sig("◓ x"), Working},
		{"claude braille (≤2.1.227)", claude, sig("⠂ Claude Code"), Working},
		{"claude ✳", claude, sig("✳ Claude Code"), Waiting},
		{"claude empty (exit)", claude, sig(""), Unknown},
		{"claude plain", claude, sig("Claude Code"), Unknown},
		{"codex spinner", codex, sig("⠋ Run sleep 5 command | repo"), Working},
		{"codex spinner alone", codex, sig("⠹ repo"), Working},
		{"codex idle", codex, sig("Run sleep 5 command | repo"), Waiting},
		{"codex action required", codex, sig("Action Required | repo"), Question},
		{"codex empty", codex, sig(""), Unknown},
		{"kimi busy", kimi, Signal{Title: "Run the shell", Progress: 3}, Working},
		{"kimi normal", kimi, Signal{Progress: 1}, Working},
		{"kimi clear", kimi, Signal{Progress: 0}, Waiting},
		{"kimi error", kimi, Signal{Progress: 2}, Unknown},
		{"kimi none", kimi, Signal{Progress: -1}, Unknown},
		{"junie title", junie, sig("◐ Junie"), Unknown},
		{"claude ignores progress", claude, Signal{Progress: 3}, Unknown},
	}
	for _, c := range cases {
		if got := SignalState(c.r, c.s); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestClassifyWith(t *testing.T) {
	t.Parallel()
	claude := DefaultRules("claude")
	idleBox := Tail("done\n────────────────────\n❯ \n────────────────────\n", 15)
	spinner := Tail("✻ Cogitating… (27s · ↓ 1.5k tokens)\n────────────────────\n❯ \n", 15)
	dialog := Tail(" Do you want to create b.txt?\n ❯ 1. Yes\n   2. Yes, and switch\n   3. No\n Esc to cancel · Tab to amend\n", 15)
	noise := Tail("just some text\nmid redraw\n", 15)
	ownMenu := Tail(" ❯ 1. View tools\n Esc to back\n", 15)
	cases := []struct {
		name     string
		lines    []string
		s        Signal
		want     State
		wantIdle bool
	}{
		{"◐ over the idle box (between two steps)", idleBox, sig("◐ x"), Working, false},
		{"◐ over noise", noise, sig("◐ x"), Working, false},
		{"dialog under ✳", dialog, sig("✳ x"), Question, true},
		{"✳ over the idle box", idleBox, sig("✳ x"), Waiting, true},
		{"✳ over noise", noise, sig("✳ x"), Waiting, true},
		{"static ✳ over a spinner", spinner, sig("✳ x"), Working, true},
		{"✳ untrusted over noise", noise, Signal{Title: "✳ x", Progress: -1}, Unknown, false},
		{"✳ untrusted over the idle box", idleBox, Signal{Title: "✳ x", Progress: -1}, Waiting, false},
		{"own menu under ✳", ownMenu, sig("✳ x"), Unknown, true},
		{"no signal", idleBox, Signal{Progress: -1}, Waiting, false},
	}
	for _, c := range cases {
		st, idle := ClassifyWith(claude, c.lines, c.s)
		if st != c.want || idle != c.wantIdle {
			t.Errorf("%s: %q idle=%v, want %q idle=%v", c.name, st, idle, c.want, c.wantIdle)
		}
	}
	codex := DefaultRules("codex")
	if st, _ := ClassifyWith(codex, idleBox, sig("Action Required | repo")); st != Question {
		t.Errorf("codex action required: %q", st)
	}
}

// The screen-only form is ClassifyWith with no signal.
func TestClassifyIsClassifyWithoutASignal(t *testing.T) {
	t.Parallel()
	r := DefaultRules("claude")
	for _, text := range []string{"✻ Cogitating… (27s · ↓ 1.5k tokens)", "x\n────────────────────\n❯ ", " ❯ 1. Yes\n Esc to cancel", "noise"} {
		lines := Tail(text, 15)
		got, _ := ClassifyWith(r, lines, Signal{Progress: -1})
		if want := Classify(r, lines); got != want {
			t.Errorf("%q: %q vs %q", text, got, want)
		}
	}
}
```

Note: "own menu under ✳" expects Unknown — an own menu keeps the previous state even when the title says idle (the user opened /config mid-idle: it stays idle anyway; opened mid-turn it is impossible since the title spins). Step 4 of the order runs only when the screen is Unknown AND not an own menu — see implementation.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/agentstate/ -count=1`
Expected: compile FAIL — `Signal`, `SignalState`, `ClassifyWith` undefined.

- [ ] **Step 3: Implement** in `classify.go`

Extend `Rules` and its doc:

```go
// Title* match the agent's window title (OSC 0/2) and ProgressBusy reads its
// OSC 9;4 report: herdr's order puts a title question and a title spinner
// above every screen rule and an idle title below them (ClassifyWith).
// Built-in only.
type Rules struct {
	Working, Waiting, Question []*regexp.Regexp
	Own                        []*regexp.Regexp
	TitleWorking, TitleQuestion, TitleIdle []*regexp.Regexp
	ProgressBusy bool // OSC 9;4: 1/3 working, 0 idle
}
```

Title defaults (beside `ownMenus`):

```go
// titleRules: [working, question, idle] title patterns per gg agent id,
// from live captures (2026-10-04). Claude Code 2.1.289 titles a turn "◐ <topic>"
// / "◑ <topic>" (◒◓ reserved; braille up to 2.1.227) and an idle or a dialog
// "✳ <topic>" — under a multiplexer always "✳", which is why title-idle is
// only a fallback. Codex 0.160.0 prefixes a braille spinner while working,
// says "Action Required" when blocked (herdr) and drops the glyph when idle.
var titleRules = map[string][3][]string{
	"claude": {{`^[◐◑◒◓⠀-⣿] `}, nil, {`^✳ `}},
	"codex":  {{`(?:^| )[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏](?: |$)`}, {`Action Required`}, {`\S`}},
}

// progressAgents report their turn through OSC 9;4 (Kimi Code 2.1.1: 3 while
// working, 0 when done).
var progressAgents = map[string]bool{"kimi": true}
```

In `DefaultRules`, after the `Own` compile:

```go
	tr := titleRules[agentID]
	if r.TitleWorking, err = compileAll(tr[0]); err != nil {
		panic(err)
	}
	if r.TitleQuestion, err = compileAll(tr[1]); err != nil {
		panic(err)
	}
	if r.TitleIdle, err = compileAll(tr[2]); err != nil {
		panic(err)
	}
	r.ProgressBusy = progressAgents[agentID]
```

New code after `Classify`:

```go
// Signal is what the agent announces outside its screen. Trusted: this
// session has shown a working title or progress before — only then does an
// idle one count (a title frozen at "✳" says nothing).
type Signal struct {
	Title    string
	Progress int // OSC 9;4 state, −1 none
	Trusted  bool
}

// SignalState is what the title and progress say on their own: Question,
// Working, Waiting (idle) or Unknown. Trusted is not consulted.
func SignalState(r Rules, s Signal) State {
	if s.Title != "" {
		switch {
		case anyMatch(r.TitleQuestion, s.Title):
			return Question
		case anyMatch(r.TitleWorking, s.Title):
			return Working
		}
	}
	if r.ProgressBusy && (s.Progress == 1 || s.Progress == 3) {
		return Working
	}
	if s.Title != "" && anyMatch(r.TitleIdle, s.Title) {
		return Waiting
	}
	if r.ProgressBusy && s.Progress == 0 {
		return Waiting
	}
	return Unknown
}

// ClassifyWith classifies the screen together with the signal, first match
// wins: a title question, a title spinner (or busy progress), the screen
// (Classify), and last a trusted idle title when the screen decides
// nothing. titleIdle reports a trusted idle title whichever step decided —
// the watcher holds such an idle for less.
func ClassifyWith(r Rules, lines []string, s Signal) (st State, titleIdle bool) {
	says := SignalState(r, s)
	if says == Question || says == Working {
		return says, false
	}
	titleIdle = says == Waiting && s.Trusted
	st = Classify(r, lines)
	if st == Unknown && titleIdle && !(len(lines) > 0 && anyMatch(r.Own, lines[len(lines)-1])) {
		st = Waiting
	}
	return st, titleIdle
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agentstate/ -count=1`
Expected: PASS (old tests too).

- [ ] **Step 5: Commit** — gofmt; `gg add internal/agentstate/classify.go internal/agentstate/<test file>`; message `feat(agentstate): title and progress rules, ClassifyWith`.

---

### Task 4: a command's screen config keeps the title rules

**Files:**
- Modify: `internal/domain/session_states.go` (`SessionRules`)
- Test: `internal/domain/session_states_test.go` (extend `TestSessionRulesMergeWithTheAgentBuiltins`)

**Interfaces:**
- Consumes: Task 3's `Rules.Title*`, `ProgressBusy`, `ClassifyWith`, `Signal`.

- [ ] **Step 1: Write the failing test** — append inside `TestSessionRulesMergeWithTheAgentBuiltins`, before the "custom command" block:

```go
	// The title rules have no config list either: a block keeps them.
	if st, _ := agentstate.ClassifyWith(r, agentstate.Tail(actIdle, 15), agentstate.Signal{Title: "◐ x", Progress: -1}); st != agentstate.Working {
		t.Fatalf("claude's title spinner with a partial block: %q", st)
	}
	kimi := config.ToolCommand{Category: "session", Name: "Kimi", Command: "kimi", ScreenQuestion: []string{`CONFIRM`}}
	if kr, _, _ := SessionRules(kimi); !kr.ProgressBusy {
		t.Fatal("kimi's progress rule lost with a partial block")
	}
```

(Check `agentIDFor` maps Command "kimi" to agent id "kimi"; if it keys on something else, build the ToolCommand the way `TestSessionRules` builds Claude's.)

- [ ] **Step 2: Run** — `go test ./internal/domain/ -run SessionRules -count=1` → FAIL (`claude's title spinner…: "waiting"`).

- [ ] **Step 3: Implement** — in `SessionRules`, after `r.Own = def.Own`:

```go
		// Nor have the title and progress rules.
		r.TitleWorking, r.TitleQuestion, r.TitleIdle, r.ProgressBusy = def.TitleWorking, def.TitleQuestion, def.TitleIdle, def.ProgressBusy
```

- [ ] **Step 4: Run** — same command → PASS.

- [ ] **Step 5: Commit** — `fix(domain): a command's screen rules keep the agent's title rules`.

---

### Task 5: the watcher uses the signal; agent_wait honours the shorter hold

**Files:**
- Modify: `internal/domain/session_states.go` (`stateSource`, `managerSource`, `SessionActivity`, `StateWatcher`, `observe`, the settle vars + `UseTitleSettle`)
- Modify: `internal/domain/agentwait.go` (settle)
- Test: `internal/domain/session_states_test.go`, `internal/domain/agentwait_test.go`

**Interfaces:**
- Consumes: `agentsession.Session.Signals()`, `agentstate.ClassifyWith/SignalState/Signal`.
- Produces: `stateSource.Signals(id agentsession.ID) (agentsession.Signals, bool)`; `SessionActivity.Settle time.Duration`; `titleSettle` (700 ms) + `UseTitleSettle(d) func()`.

- [ ] **Step 1: Write the failing tests**

`session_states_test.go` — extend `fakeStates`:

```go
type fakeStates struct {
	infos []agentsession.Info
	text  map[agentsession.ID]string
	last  map[agentsession.ID]time.Time
	sig   map[agentsession.ID]agentsession.Signals // absent: no title, no progress
	slow  chan struct{}
}

func (f *fakeStates) Signals(id agentsession.ID) (agentsession.Signals, bool) {
	if s, ok := f.sig[id]; ok {
		return s, true
	}
	return agentsession.Signals{Progress: -1}, true
}
```

and initialise `sig: map[agentsession.ID]agentsession.Signals{}` in `oneSession`. New tests:

```go
func title(t string) agentsession.Signals { return agentsession.Signals{Title: t, Progress: -1} }

// Between two steps Claude's screen shows the empty prompt while its title
// still spins: the row stays working and nothing is pending.
func TestStatesTitleWorkingBridgesTheGap(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("◐ topic")
	w.observe(late)
	f.text["s1"] = actIdle
	if wait := w.observe(late.Add(time.Second)); wait != 0 {
		t.Fatalf("an idle is pending under a spinning title: %v", wait)
	}
	w.observe(late.Add(5 * time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityWorking || kinds(w.Notices(0)) != "" {
		t.Fatalf("activity %+v notices %q", a, kinds(w.Notices(0)))
	}
}

// The title says idle: the hold is titleSettle, the idle shows from its
// first read and carries the hold it passed.
func TestStatesTitledIdleHoldsShort(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("◑ topic")
	w.observe(late)
	f.text["s1"], f.sig["s1"] = actIdle, title("✳ topic")
	if wait := w.observe(late.Add(time.Second)); wait != 700*time.Millisecond {
		t.Fatalf("titled idle recheck = %v, want 700ms", wait)
	}
	w.observe(late.Add(1700 * time.Millisecond))
	a, _ := w.Get("s1")
	if a.State != ActivityIdle || !a.Since.Equal(late.Add(time.Second)) || a.Settle != 700*time.Millisecond {
		t.Fatalf("activity = %+v", a)
	}
	if kinds(w.Notices(0)) != "idle" {
		t.Fatalf("notices = %q", kinds(w.Notices(0)))
	}
}

// A title that never animated ("✳" from the start: a multiplexer gg did not
// strip) says nothing: today's 2 s hold applies.
func TestStatesNeverAnimatedKeeps2s(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("✳ topic")
	w.observe(late)
	f.text["s1"] = actIdle
	if wait := w.observe(late.Add(time.Second)); wait != 2*time.Second {
		t.Fatalf("recheck = %v, want 2s", wait)
	}
	w.observe(late.Add(3 * time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityIdle || a.Settle != 2*time.Second {
		t.Fatalf("activity = %+v", a)
	}
}

// A dialog mid-turn: the title turns "✳", the screen says question — at once.
func TestStatesDialogUnderAnIdleTitleIsAQuestion(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("◐ topic")
	w.observe(late)
	f.text["s1"], f.sig["s1"] = actQuestion, title("✳ topic")
	w.observe(late.Add(time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityQuestion || len(a.Options) == 0 {
		t.Fatalf("activity = %+v", a)
	}
}

// A redraw the screen rules cannot read, under a trusted idle title, still
// becomes idle (after the short hold).
func TestStatesTitledIdleOverAnUnreadableScreen(t *testing.T) {
	t.Parallel()
	w, f := oneSession("claude")
	late := actT0.Add(time.Minute)
	f.text["s1"], f.sig["s1"] = actWorking, title("◐ topic")
	w.observe(late)
	f.text["s1"], f.sig["s1"] = actNoise, title("✳ topic")
	w.observe(late.Add(time.Second))
	w.observe(late.Add(2 * time.Second))
	if a, _ := w.Get("s1"); a.State != ActivityIdle {
		t.Fatalf("activity = %+v", a)
	}
}

// Kimi's progress report: busy keeps working over an idle box, clear goes
// idle on the short hold.
func TestStatesKimiProgress(t *testing.T) {
	t.Parallel()
	w, f := oneSession("kimi")
	late := actT0.Add(time.Minute)
	kimiIdle := " │ >                    │\n ╰────────────────────╯\n"
	f.text["s1"], f.sig["s1"] = kimiIdle, agentsession.Signals{Progress: 3}
	w.observe(late)
	if a, _ := w.Get("s1"); a.State != ActivityWorking {
		t.Fatalf("busy: %+v", a)
	}
	f.sig["s1"] = agentsession.Signals{Progress: 0}
	if wait := w.observe(late.Add(time.Second)); wait != 700*time.Millisecond {
		t.Fatalf("recheck = %v", wait)
	}
}
```

(Check the kimi idle box against `defaults["kimi"]` waiting rule `^│ >[^\n]*\n╰` — `Tail` trims leading spaces, so the fixture lines start with `│` / `╰` after trimming.)

`agentwait_test.go`:

```go
// The watcher's activity says which hold its idle passed; agent_wait trusts
// the idle after that, not after idleSettle.
func TestAgentWaitHonoursTheIdlesSettle(t *testing.T) {
	ov, w1, _, s1, _, w := waitFixture(t)
	t.Cleanup(UseIdleSettle(2 * time.Second))
	start := time.Now()
	setStatic(w, s1.Info().ID, SessionActivity{State: ActivityIdle, Since: start, Settle: 100 * time.Millisecond})
	res := doWait(t, ov, w1, "idle", 3*time.Second)
	if res.TimedOut || time.Since(start) > time.Second {
		t.Fatalf("titled idle waited for idleSettle: %+v after %v", res, time.Since(start))
	}
}
```

(Serial — `waitFixture` swaps globals; no `t.Parallel()`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain/ -run 'States|AgentWait' -count=1`
Expected: compile FAIL — `a.Settle undefined` / `fakeStates` does not implement `Signals` until the interface has it; after adding the field-only change, the behaviour tests fail (e.g. `an idle is pending under a spinning title: 2s`).

- [ ] **Step 3: Implement**

Settle vars (beside `idleSettle`):

```go
	// titleSettle: the idle hold when the agent's title (or progress report)
	// says idle too — the title kept the turn working, so a short guard
	// against a redraw is enough (herdr holds 700 ms).
	titleSettle = 700 * time.Millisecond
```

```go
// UseTitleSettle replaces the titled idle hold (tests) and returns the
// restore func.
func UseTitleSettle(d time.Duration) func() {
	statesMu.Lock()
	prev := titleSettle
	titleSettle = d
	statesMu.Unlock()
	return func() { statesMu.Lock(); titleSettle = prev; statesMu.Unlock() }
}
```

(Match `UseIdleSettle`'s exact shape — read it first.)

`SessionActivity`:

```go
	Settle  time.Duration    // the hold an idle passed (agent_wait trusts it after this); 0 otherwise
```

`stateSource` + `managerSource`:

```go
	Signals(id agentsession.ID) (agentsession.Signals, bool)
```

```go
func (s managerSource) Signals(id agentsession.ID) (agentsession.Signals, bool) {
	if sess, ok := s.m.Get(id); ok {
		return sess.Signals(), true
	}
	return agentsession.Signals{}, false
}
```

`StateWatcher` field (beside `progress`), initialised in `newStateWatcher`:

```go
	// animated: sessions whose title or progress has said working — only
	// their idle title counts (agentstate.Signal.Trusted).
	animated map[SessionID]bool
```

`observe`:

1. Read settles: `grace, stall, spinStall, settle, tsettle := stateGrace, stallAfter, spinStallAfter, idleSettle, titleSettle`.
2. Before the read loop, copy the guard under `w.mu`:
   ```go
   w.mu.Lock()
   animated := maps.Clone(w.animated)
   w.mu.Unlock()
   ```
   (add `"maps"` to the imports)
3. `reading` gains `says agentstate.State` and `titleIdle bool`. In the loop, after `lines`:
   ```go
   sigs, _ := w.src.Signals(info.ID)
   sg := agentstate.Signal{Title: sigs.Title, Progress: sigs.Progress}
   says := agentstate.SignalState(rules, sg)
   sg.Trusted = animated[info.ID] || says == agentstate.Working
   st, titleIdle := agentstate.ClassifyWith(rules, lines, sg)
   ```
   and store `st`, `says`, `titleIdle` in the reading (replacing `agentstate.Classify(rules, lines)`).
4. In the locked loop, first: `if rd.says == agentstate.Working { w.animated[info.ID] = true }`.
5. Replace the pending-idle case:
   ```go
   		hold := settle
   		if rd.titleIdle {
   			hold = tsettle
   		}
   		switch {
   		case st == agentstate.Waiting && prev.State == agentstate.Working:
   			first, ok := w.pendingIdle[info.ID]
   			if !ok {
   				first, w.pendingIdle[info.ID] = now, now
   			}
   			if held := now.Sub(first); held < hold {
   				st = agentstate.Unknown // not yet: the session stays working
   				if left := hold - held; recheck == 0 || left < recheck {
   					recheck = left
   				}
   			} else {
   				delete(w.pendingIdle, info.ID)
   				since = first
   			}
   		case st != agentstate.Unknown:
   			delete(w.pendingIdle, info.ID) // working again, or a question
   		}
   		if st != agentstate.Unknown && st != prev.State {
   			next.State, next.Since, next.Settle = st, since, 0
   			if st == agentstate.Waiting {
   				next.Settle = hold
   			}
   			...existing notices...
   ```
6. In the dead-session cleanup add `delete(w.animated, id)`.
7. Update `observe`'s doc comment: the hold is `titleSettle` when the title says idle too, `idleSettle` otherwise; a spinning title keeps a session working whatever the screen shows between two steps.

`agentwait.go`:

```go
	if want["idle"] && a.State == ActivityIdle && now.Sub(a.Since) >= idleHold(a, settle) && !m.idleSince.Equal(a.Since) {
```

```go
// idleHold is how long agent_wait sees an idle held before trusting it: the
// hold the watcher applied (shorter under an idle title), else idleSettle.
func idleHold(a SessionActivity, settle time.Duration) time.Duration {
	if a.Settle > 0 {
		return a.Settle
	}
	return settle
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/domain/ -run 'States|AgentWait|SessionRules' -count=1 -race`
Expected: PASS — including the existing `TestStatesIdleShowsOnlyOnceItHolds` (no title → 2 s) and `TestAgentWaitIdleSettle` (Settle 0 → idleSettle).

- [ ] **Step 5: Whole-package run** — `go test ./internal/domain/ ./internal/agentsession/ ./internal/agentstate/ ./internal/tui/ ./internal/web/ ./internal/mcp/ -count=1` → PASS (other implementations of `stateSource`, if any, fail to compile here — add `Signals` to them).

- [ ] **Step 6: Commit** — gofmt; `gg add` the four files; message `feat(domain): agent states read the title; a titled idle holds 700 ms`.

---

### Task 6: docs, race gate, live check

**Files:**
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` (agentstate + agentsession rows, one line each)

- [ ] **Step 1: Docs**
  - `CHANGELOG.md` (top, Unreleased/newest section, match the existing entry style): "Agent sessions — the title says working" — Claude/Codex rows stay working for a whole turn and turn idle ~0.7 s after it ends (was 2 s); Kimi via its progress report; Junie/agy unchanged; STY/ZELLIJ no longer reach agents.
  - `docs/CLAUDE-details.md`: a section "Agent states from the title (2026-10-04)": the capture table, Claude's mux rule (`TMUX`/`STY`/`ZELLIJ` → static ✳), the order in `ClassifyWith`, the Trusted guard, `titleSettle` + `SessionActivity.Settle` + agent_wait, the oscFilter recording (OSC 0/2/9 only, 256-byte cap).
  - `CLAUDE.md`: agentstate row — mention `ClassifyWith` (title/progress signal: title question → title working → screen → idle title); agentsession row — `Signals` (title + OSC 9;4). Keep each row one line.
  - No CLI surface change → no skill bump.

- [ ] **Step 2: Commit** — `docs: agent states from the title — CHANGELOG, details, map`.

- [ ] **Step 3: Race gate** — `./test.sh race > <scratch>/race.log 2>&1`; read its tail; green only on the literal `all green`.

- [ ] **Step 4: Live check** — `go build -o bin/gg ./cmd/gg` in the worktree; in an own tmux session `claude-titlecheck` run `<worktree>/bin/gg` in a scratch repo, open a Claude console (read every screen before each key), send a multi-tool prompt; watch the Worktrees row badge: working throughout (no idle between steps), idle ~1 s after the turn ends; a Write permission dialog shows as question. Record what was seen in the ledger. Kill only `claude-titlecheck`.

- [ ] **Step 5:** final whole-branch review (read-only subagent on Fable), fix pass, ask before merging.
