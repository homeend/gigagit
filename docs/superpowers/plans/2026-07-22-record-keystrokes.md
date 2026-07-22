# `gg --record` Keystroke Recorder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg --record <file>` records a TUI session's keystrokes to a file in the `tui-capture.sh` keyscript format, so a human-driven scenario can be replayed headlessly and every screen captured.

**Architecture:** A `*recorder` pointer field on the value-receiver TUI `Model` (the `modal`/`popup` pattern); one tap line at the `tea.KeyMsg` case in `Update` translates each key to a tui-capture token and writes it. The `--record` flag is pulled off `os.Args` like `--time-track` and threaded into `tui.Run`. A one-line `#`-comment skip in `tui-capture.sh` lets the recorder's header round-trip.

**Tech Stack:** Go 1.26, Bubble Tea (`tea.KeyMsg`), gg's existing TUI test helpers; bash for the capture-side change.

Spec: `docs/superpowers/specs/2026-07-22-record-keystrokes-design.md`.

## Global Constraints

- **Worktree:** ALL work happens in `/mnt/t/others/gigagit.worktrees/feat-record-keystrokes` on branch `feat/record-keystrokes`. Subagents start in the main checkout — `cd` to the worktree FIRST and verify: `git branch --show-current` prints `feat/record-keystrokes`. Write/Edit tools need the worktree's ABSOLUTE paths.
- **Domain-only import rule stays:** `internal/tui` must NOT import `internal/git` (it reaches git through `internal/domain`). The recorder imports only stdlib + bubbletea.
- **No new module dependencies:** `go.mod`/`go.sum` must not change.
- **The token vocabulary is the round-trip contract:** every token `keyToken` returns with `ok==true` MUST be one `tui-capture.sh`'s `send_tokens` accepts — the named set `enter esc space tab up down left right bspace`, a `C-<x>`/`M-<x>` chord, or any literal rune (sent via `send-keys -l`). Do not invent a named token (`pgup`, `home`, …) that `send_tokens` would mis-send as literal text; unsupported keys are recorded as `#` comments instead.
- **Commits:** use `gg add <paths>` + `gg commit -m "..."` (dogfood). Every commit message ends with the two trailer lines shown in the commit steps.
- **After each Go task:** `gofmt -l internal/tui cmd/gg` prints nothing; `go build ./cmd/gg` succeeds; `go test ./internal/tui/` passes.
- Tests follow gg's TUI patterns: `New(nil)` for a service-less model, `keyMsg(s)`/`tea.KeyMsg` literals for keypresses (`internal/tui/model_test.go`).

---

### Task 1: `internal/tui/recorder.go` — keyToken + recorder (isolated)

**Files:**
- Create: `internal/tui/recorder.go`
- Create: `internal/tui/recorder_test.go`

**Interfaces:**
- Consumes: bubbletea `tea.KeyMsg`.
- Produces (Task 2 relies on these): `type recorder struct{…}`; `func newRecorder(path, repo string) (*recorder, error)`; `func (r *recorder) note(msg tea.KeyMsg)` (nil-safe); `func (r *recorder) close()` (nil-safe); `func keyToken(msg tea.KeyMsg) (string, bool)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/recorder_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyToken(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		tok  string
		ok   bool
	}{
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "enter", true},
		{"esc", tea.KeyMsg{Type: tea.KeyEsc}, "esc", true},
		{"space", tea.KeyMsg{Type: tea.KeySpace}, "space", true},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "tab", true},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "up", true},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, "down", true},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "left", true},
		{"right", tea.KeyMsg{Type: tea.KeyRight}, "right", true},
		{"bspace", tea.KeyMsg{Type: tea.KeyBackspace}, "bspace", true},
		{"dot", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")}, ".", true},
		{"letter", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}, "a", true},
		{"question", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}, "?", true},
		{"ctrl-g", tea.KeyMsg{Type: tea.KeyCtrlG}, "C-g", true},
		{"pgup-unsupported", tea.KeyMsg{Type: tea.KeyPgUp}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok, ok := keyToken(c.msg)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (tok %q)", ok, c.ok, tok)
			}
			if c.ok && tok != c.tok {
				t.Errorf("tok = %q, want %q", tok, c.tok)
			}
		})
	}
}

// The named tokens keyToken emits must be exactly the named keys
// tui-capture.sh's send_tokens recognizes (else replay mis-sends them as
// literal text). This is the round-trip contract, kept in lockstep by hand.
func TestKeyTokenNamedVocabulary(t *testing.T) {
	captureNamed := map[string]bool{
		"enter": true, "esc": true, "space": true, "tab": true,
		"up": true, "down": true, "left": true, "right": true, "bspace": true,
	}
	named := []tea.KeyMsg{
		{Type: tea.KeyEnter}, {Type: tea.KeyEsc}, {Type: tea.KeySpace},
		{Type: tea.KeyTab}, {Type: tea.KeyUp}, {Type: tea.KeyDown},
		{Type: tea.KeyLeft}, {Type: tea.KeyRight}, {Type: tea.KeyBackspace},
	}
	for _, m := range named {
		tok, ok := keyToken(m)
		if !ok || !captureNamed[tok] {
			t.Errorf("named key %v -> %q (ok %v): not in send_tokens' vocabulary", m.Type, tok, ok)
		}
	}
}

// nonCommentLines returns the non-empty, non-`#` lines of a recording.
func nonCommentLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		out = append(out, ln)
	}
	return out
}

func TestRecorderHeaderBodyAndDroppedQuit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.keys")
	r, err := newRecorder(path, "/some/repo")
	if err != nil {
		t.Fatal(err)
	}
	// . down enter q   (q is the terminating quit)
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	r.note(tea.KeyMsg{Type: tea.KeyDown})
	r.note(tea.KeyMsg{Type: tea.KeyEnter})
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	r.close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "# gg keystroke recording") || !strings.Contains(got, "# repo: /some/repo") {
		t.Errorf("header missing in:\n%s", got)
	}
	body := nonCommentLines(got)
	want := []string{".", "down", "enter"} // q dropped (terminating quit)
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
}

func TestRecorderCommentsUnsupportedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scenario.keys")
	r, _ := newRecorder(path, "repo")
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	r.note(tea.KeyMsg{Type: tea.KeyPgUp}) // unsupported -> comment
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	r.note(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) // quit
	r.close()

	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, "# unrecorded key:") {
		t.Errorf("expected an unrecorded-key comment, got:\n%s", s)
	}
	if body := nonCommentLines(s); !reflect.DeepEqual(body, []string{"a", "b"}) {
		t.Errorf("body = %v, want [a b]", body)
	}
}

func TestRecorderNilSafe(t *testing.T) {
	var r *recorder
	r.note(tea.KeyMsg{Type: tea.KeyEnter}) // must not panic
	r.close()                              // must not panic
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-record-keystrokes && go test ./internal/tui/ -run 'TestKeyToken|TestRecorder'`
Expected: FAIL — `undefined: keyToken` / `undefined: newRecorder` (compile error).

- [ ] **Step 3: Write the implementation**

Create `internal/tui/recorder.go`:

```go
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// recorder appends a TUI session's keystrokes to a file in the tui-capture.sh
// keyscript format (one token per line), so the session can be replayed
// headlessly. It lags by one key so the terminating quit (q / ctrl+c) is
// never written. Best-effort: a write error disables it rather than
// disturbing the live session. A nil *recorder is a no-op.
type recorder struct {
	f       *os.File
	pending string // buffered token (lag-by-one)
	has     bool   // whether pending holds a real token
	broken  bool   // a write failed; stop recording
}

// newRecorder creates/truncates path and writes a self-documenting comment
// header (which repo, and when) so a scenario file records its own context.
func newRecorder(path, repo string) (*recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	r := &recorder{f: f}
	r.writeLine("# gg keystroke recording")
	r.writeLine("# repo: " + repo)
	r.writeLine("# recorded: " + time.Now().UTC().Format(time.RFC3339))
	return r, nil
}

func (r *recorder) writeLine(s string) {
	if r.broken {
		return
	}
	if _, err := fmt.Fprintln(r.f, s); err != nil {
		r.broken = true
	}
}

// note records one keypress. A supported key is buffered (lag-by-one) so the
// final quit can be dropped at close. An unsupported key flushes any buffered
// token, then writes a replay-skipped `#` comment so the scenario stays honest.
func (r *recorder) note(msg tea.KeyMsg) {
	if r == nil || r.broken {
		return
	}
	tok, ok := keyToken(msg)
	if !ok {
		if r.has {
			r.writeLine(r.pending)
			r.has = false
		}
		r.writeLine("# unrecorded key: " + msg.String())
		return
	}
	if r.has {
		r.writeLine(r.pending)
	}
	r.pending, r.has = tok, true
}

// close closes the file WITHOUT flushing the buffered token — that token is
// the session-terminating quit, which must not appear in a replayable script.
func (r *recorder) close() {
	if r == nil {
		return
	}
	_ = r.f.Close()
}

// keyToken maps a bubbletea key to a tui-capture token. ok is false for a key
// outside send_tokens' vocabulary (page keys, home/end, function keys, …); the
// caller records those as comments. Every ok==true token is one send_tokens
// accepts: a named key, a C-/M- chord, or a literal rune.
func keyToken(msg tea.KeyMsg) (string, bool) {
	switch msg.Type {
	case tea.KeyRunes:
		return string(msg.Runes), true
	case tea.KeyEnter:
		return "enter", true
	case tea.KeyEsc:
		return "esc", true
	case tea.KeySpace:
		return "space", true
	case tea.KeyTab:
		return "tab", true
	case tea.KeyUp:
		return "up", true
	case tea.KeyDown:
		return "down", true
	case tea.KeyLeft:
		return "left", true
	case tea.KeyRight:
		return "right", true
	case tea.KeyBackspace:
		return "bspace", true
	}
	s := msg.String()
	if strings.HasPrefix(s, "ctrl+") {
		return "C-" + strings.TrimPrefix(s, "ctrl+"), true
	}
	if strings.HasPrefix(s, "alt+") {
		return "M-" + strings.TrimPrefix(s, "alt+"), true
	}
	return "", false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestKeyToken|TestRecorder'`
Expected: PASS. Also `gofmt -l internal/tui/recorder.go internal/tui/recorder_test.go` prints nothing.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-record-keystrokes
gg add internal/tui/recorder.go internal/tui/recorder_test.go
gg commit -m "feat(tui): keystroke recorder — keyToken + lag-drop-quit recorder

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG"
```

---

### Task 2: wiring — Model field, Update tap, `tui.Run`, `--record` flag

**Files:**
- Modify: `internal/tui/model.go` (add `recorder *recorder` field; add the tap in the `KeyMsg` case)
- Modify: `internal/tui/run.go` (Run signature + construct/attach/close the recorder)
- Modify: `cmd/gg/main.go` (`extractRecord`, validation, `tui.Run` call)
- Modify: `internal/tui/recorder_test.go` (append the Model-tap test)

**Interfaces:**
- Consumes: `newRecorder`, `note`, `close` from Task 1; `New(svc)`, `Model.Update`, `keyMsg` test helper.
- Produces: `tui.Run(svc *domain.Service, recordPath string) (string, error)`; `extractRecord(args []string) (string, []string)`; `checkRecordPath(path string) error`.

- [ ] **Step 1: Write the failing Model-tap test**

Append to `internal/tui/recorder_test.go`:

```go
// The Update tap must record a keystroke. Because recorder is a pointer field,
// both Update calls share it even though Update has a value receiver; the first
// key is flushed when the second arrives, and the second (buffered) is dropped
// at close — so exactly the first key lands in the file.
func TestUpdateRecordsKeystroke(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.keys")
	r, err := newRecorder(path, "repo")
	if err != nil {
		t.Fatal(err)
	}
	m := New(nil)
	m.recorder = r
	m.Update(keyMsg("down")) // recorded (buffered)
	m.Update(keyMsg("down")) // flushes the first; buffers the second
	r.close()                // drops the buffered second

	got, _ := os.ReadFile(path)
	if body := nonCommentLines(string(got)); !reflect.DeepEqual(body, []string{"down"}) {
		t.Errorf("body = %v, want [down]", body)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestUpdateRecordsKeystroke`
Expected: FAIL — `m.recorder undefined` (field not on Model yet).

- [ ] **Step 3: Add the Model field**

In `internal/tui/model.go`, in the `Model` struct, next to the other pointer fields (immediately after the `modal *decisionState` line, ~line 182), add:

```go
	recorder *recorder // keystroke recorder (nil unless gg --record)
```

- [ ] **Step 4: Add the tap in Update**

In `internal/tui/model.go`, inside `case tea.KeyMsg:`, DIRECTLY AFTER the space-normalization block (the `if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == ' ' { … }` block) and BEFORE the `// The status line holds a transient message` comment, insert:

```go
		// Record the (normalized) keypress before it is handled, when gg --record
		// is active. note() is nil-safe, so this is a no-op in the common case.
		m.recorder.note(msg)
```

- [ ] **Step 5: Thread the recorder through `tui.Run`**

In `internal/tui/run.go`, change the signature and construct/attach/close the recorder. Replace the `func Run(svc *domain.Service) (string, error) {` line with:

```go
func Run(svc *domain.Service, recordPath string) (string, error) {
```

Directly after `m = m.initSnapshotTarget()` (just before `p := tea.NewProgram(...)`), add:

```go
	if recordPath != "" {
		repo := ""
		if top, err := svc.TopLevel(context.Background()); err == nil {
			repo = top
		}
		rec, rerr := newRecorder(recordPath, repo)
		if rerr != nil {
			return "", fmt.Errorf("--record: %w", rerr)
		}
		m.recorder = rec
	}
```

Add `"fmt"` to `run.go`'s imports (it is not currently imported).

In the existing `if fm, ok := final.(Model); ok {` block (after `p.Run()`), add a line so the recorder is closed on exit:

```go
		fm.recorder.close()
```

- [ ] **Step 6: Wire the flag in `cmd/gg/main.go`**

(a) Add the extractor. Directly after the `extractTimeTrack` function (ends ~line 168), add:

```go
// extractRecord pulls the global --record flag (in either "--record path" or
// "--record=path" form) out of args, returning its value and the remaining
// args. A trailing "--record" with no value is dropped.
func extractRecord(args []string) (string, []string) {
	path := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--record":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--record="):
			path = strings.TrimPrefix(a, "--record=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest
}

// checkRecordPath verifies the --record file can be created, so a bad path is
// reported cleanly before the TUI takes over the screen.
func checkRecordPath(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}
```

(b) Extract the flag. Directly after `timeTrack, args := extractTimeTrack(args)` (line 29), add:

```go
	recordPath, args := extractRecord(args)
```

(c) Validate + pass it in. Replace the `cwd, err := tui.Run(svc)` line (line 110) with:

```go
	if recordPath != "" {
		if err := checkRecordPath(recordPath); err != nil {
			fmt.Fprintln(os.Stderr, "gg: --record:", err)
			os.Exit(2)
		}
	}
	cwd, err := tui.Run(svc, recordPath)
```

- [ ] **Step 7: Build, run the Model-tap test, and smoke-test the flag**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-record-keystrokes
gofmt -l internal/tui cmd/gg          # must print nothing
go build -o /tmp/ggrec ./cmd/gg
go test ./internal/tui/ -run TestUpdateRecordsKeystroke
```
Expected: build succeeds; the tap test PASSES.

Then a real end-to-end smoke test via the merged capture harness (record is driven headlessly here by tmux): the recorder writes as keys arrive, so drive gg under tmux, send a couple keys + quit, and inspect the file:

```bash
REC=$(mktemp); SESS=ggrec_smoke
tmux kill-session -t $SESS 2>/dev/null
tmux new-session -d -s $SESS -x 120 -y 40 -c "$(pwd)" "/tmp/ggrec --record $REC"
sleep 2
tmux send-keys -t $SESS "."; sleep 1
tmux send-keys -t $SESS Escape; sleep 1
tmux send-keys -t $SESS "q"; sleep 1
tmux kill-session -t $SESS 2>/dev/null
echo "=== recorded file ==="; cat "$REC"
```
Expected: the file has the `#` header, then body lines `.` and `esc` (the trailing `q` dropped). If `q` appears or the body is empty, the lag/close logic is wrong — fix before committing.

- [ ] **Step 8: Full package test**

Run: `go test ./internal/tui/`
Expected: PASS (the new field/tap/signature don't disturb existing tests).

- [ ] **Step 9: Commit**

```bash
gg add internal/tui/model.go internal/tui/run.go cmd/gg/main.go internal/tui/recorder_test.go
gg commit -m "feat(tui): wire gg --record — flag, Run threading, Update tap

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG"
```

---

### Task 3: `tui-capture.sh` — skip `#` comment lines

**Files:**
- Modify: `tui-capture.sh` (the keyscript walk loop)

**Interfaces:** none — makes the recorder's `#` header round-trip.

- [ ] **Step 1: Write the failing acceptance check**

A keyscript with a `#` header currently sends the header as literal keystrokes. Confirm the skip isn't there yet:

Run:
```bash
cd /mnt/t/others/gigagit.worktrees/feat-record-keystrokes
go build -o /tmp/ggrec ./cmd/gg
OUTD=$(mktemp -d)
printf '# header line\n. \n' > /tmp/ks.txt
./tui-capture.sh --gg /tmp/ggrec --out "$OUTD" "$(cat /tmp/ks.txt)" >/dev/null 2>&1
ls "$OUTD"
```
Expected (pre-fix): there is a `snap-01-*` for the `# header line` step (the comment was sent as keystrokes) in addition to the real `.` step — i.e. MORE snapshots than the one real step. (The `#` line was treated as input.)

- [ ] **Step 2: Add the skip (hash + SPACE, not bare `#`)**

In `tui-capture.sh`, in the keyscript-walk `while IFS= read -r step` loop, directly AFTER the trim lines and the existing `[[ -z "$step" ]] && continue`, add:

```bash
    # Skip comment lines. The recorder writes every comment as "# <text>"
    # (hash + space), so match that exactly — a lone "#" step is a real
    # literal '#' keystroke (gg binds # to goto-commit) and must NOT be
    # skipped, or that keystroke would silently vanish on replay.
    [[ "$step" == "# "* ]] && continue
```

- [ ] **Step 3: Verify (header skipped, literal `#` preserved)**

Run: `bash -n tui-capture.sh && echo "syntax ok"` → `syntax ok`.

Then re-run the Step-1 commands. Expected (post-fix): only `snap-00-init.txt` and `snap-01-step1.txt` (the real `.`), and NO snapshot for the `# header line` — the hash-space header was skipped. Confirm with `ls "$OUTD"`.

Then verify a **literal `#` keystroke is NOT skipped** (the round-trip fix for gg's `#` binding):

```bash
OUTD2=$(mktemp -d)
printf '# a header\n#\n. \n' > /tmp/ks2.txt
./tui-capture.sh --gg /tmp/ggrec --out "$OUTD2" "$(cat /tmp/ks2.txt)" >/dev/null 2>&1
ls "$OUTD2"
```
Expected: `snap-00-init.txt`, `snap-01-step1.txt` (the bare `#` line — a real keystroke, sent), and `snap-02-step2.txt` (the `.`). The `# a header` line (hash-space) is skipped; the lone `#` line is replayed. If the lone `#` is missing (only two snapshots), the pattern is wrong — it must be `"# "*` (hash-space), never `\#*`.

- [ ] **Step 4: Commit**

```bash
gg add tui-capture.sh
gg commit -m "feat(tooling): tui-capture skips # comment lines (recorder header round-trip)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG"
```

---

### Task 4: docs + full verification

**Files:**
- Modify: `.claude/skills/driving-tui-headless/SKILL.md` (add a "Recording scenarios" section)
- Modify: `CLAUDE.md`
- Modify: `CHANGELOG.md`

**Interfaces:** none — documentation + the final gate.

- [ ] **Step 1: Add the skill section**

In `.claude/skills/driving-tui-headless/SKILL.md`, add a new section (place it after the "Invoke" section, before "Keyscript"):

```markdown
## Recording a scenario (the input side)

To author a scenario instead of hand-writing a keyscript, a human runs the
TUI with `gg --record <file>`, drives it normally, and quits (`q`). gg writes
every keystroke to `<file>` in this exact keyscript format (one token per
line), with a `#` header naming the repo it was recorded against. The
terminating quit is not written. Hand that file straight to
`tui-capture.sh <file>` (via `--repo <the header's repo>`) to replay it and
capture a snapshot of every screen. Mouse clicks and page/function keys are
not recorded (they appear as `# unrecorded key:` comments); keep scenarios
keyboard-driven with the vocabulary above.
```

- [ ] **Step 2: Add the CLAUDE.md pointer**

In `CLAUDE.md`, find the paragraph added earlier that mentions `tui-capture.sh` (right after the `## Build / test` fenced block). Directly after that paragraph, add:

```markdown
To author a scenario for that harness, `gg --record <file>` dumps a live TUI
session's keystrokes to `<file>` in the same keyscript format (quit excluded);
replay it with `tui-capture.sh`.
```

- [ ] **Step 3: Add the CHANGELOG entry**

In `CHANGELOG.md`, under the current `## Unreleased` section (the same one the `tui-capture` entry lives in — the tui-capture work is merged), add:

```markdown
- `gg --record <file>` — record a TUI session's keystrokes to a file in the
  `tui-capture.sh` keyscript format (one step per key, terminating quit
  excluded, a `#` header naming the repo), so a human can author a scenario
  for headless replay + screen capture.
```

- [ ] **Step 4: Full suite**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-record-keystrokes && ./test.sh 2>&1 | tail -3`
Expected: `all green` — vet+gofmt clean, all unit tests (incl. `internal/tui`) pass, e2e passes.

- [ ] **Step 5: Commit**

```bash
gg add .claude/skills/driving-tui-headless/SKILL.md CLAUDE.md CHANGELOG.md
gg commit -m "docs: gg --record in the driving-tui-headless skill, CLAUDE.md, changelog

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG"
```

---

## After the plan

Recorder + player now form a loop: a human drives `gg --record scenario.keys`,
hands Claude the file, and `tui-capture.sh scenario.keys` replays it and
captures every screen. The natural next layer (future) is a record-once /
replay-in-CI / diff-snapshots regression harness.
