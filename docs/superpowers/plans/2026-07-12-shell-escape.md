# Shell Escape (ctrl+o subshell + palette run-command) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `ctrl+o` from any gg surface suspends the TUI into an interactive `$SHELL` in the worktree (exit returns + full reload), and the command palette gains "Open shell" + "Run shell command…" (a one-off command with a press-enter-to-return pause).

**Architecture:** One new file `internal/tui/shell_escape.go` holds pure builders (shell resolution, wrapper scripts, `*exec.Cmd` construction — the `tool_run.go` `toolScript`/`toolExecCmd` idiom), the Model glue (`openSubshell`/`runShellCommand`/`handleShellDone` over `tea.ExecProcess`, the `$EDITOR` handover precedent), and the `shellCmdPopup` (the `repoPathPopup` pattern + the existing search-history recall). `model.go` gets a central `ctrl+o` hook ABOVE the process/layer routing (unlike `ctrl+p`, which sits below `m.proc` — that placement is what makes ctrl+o reach the conflict process's "Resolve failed" screen) and a `shellDoneMsg` case.

**Tech Stack:** Go 1.26, Bubble Tea (value-receiver Model, `tea.ExecProcess`), real-exec unit tests on POSIX shapes.

**Spec:** `docs/superpowers/specs/2026-07-12-shell-escape-design.md`

## Global Constraints

- **Work ONLY in the feature worktree:** `/mnt/t/others/gigagit/.claude/worktrees/shell-escape` (branch `feat/shell-escape`). Never touch the shared checkout at `/mnt/t/others/gigagit`. All `Write`/`Edit` calls use the worktree's absolute paths; every shell command `cd`s into the worktree first. Verify `git branch --show-current` → `feat/shell-escape` before the first change.
- `internal/tui` never imports `internal/git`/`internal/shelf` in non-test files (archtest). This feature needs neither.
- No engine op, no `internal/domain` change, no repogate reservation — the `$EDITOR` standing (`edit_actions.go`/`tool_run.go` precedent). No approval gate: the user types the command at the moment of execution.
- ctrl+o gate is `opsIdle()` ONLY; busy → status notice `an operation is running — shell available when it finishes` (exact copy).
- User-visible strings in this plan are exact copy — do not reword. Banner: `gg subshell — 'exit' returns to gg` (POSIX) / `gg subshell - 'exit' returns to gg` (Windows echo, ASCII hyphen). One-off pause: `[exit %s] press enter to return to gg` (POSIX printf) / `[exit %RC%] press any key to return to gg` (Windows).
- A non-zero EXIT of the shell/command is NOT an error (`*exec.ExitError` → success path); only a launch failure surfaces as `shell: <err>`.
- Both exec cmds run with `Dir` = current worktree and `Env` = `os.Environ()` + `GG=1`.
- TDD; one commit per task; every commit message ends with the two trailer lines:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9`

---

### Task 1: Pure builders — shell resolution, wrapper scripts, exec construction

**Files:**
- Create: `internal/tui/shell_escape.go`
- Test: `internal/tui/shell_escape_test.go` (new)

**Interfaces:**
- Consumes: nothing project-specific (mirrors `tool_run.go`'s `toolScript`/`toolExecCmd` idiom — read them for style, lines 153–222).
- Produces (Task 2/3 rely on these exact signatures):
  - `func shellEscapeBin(goos string, getenv func(string) string) string` — `$SHELL` fallback `/bin/sh`; on `"windows"`: `COMSPEC` fallback `cmd`.
  - `func shellScriptFile(body string) (string, error)` — temp script (0700, `.sh`/`.bat` by `runtime.GOOS`), returns path.
  - `func subshellExec(dir string, getenv func(string) string) (*exec.Cmd, string, error)` — (cmd, scriptPath — `""` on Windows, err).
  - `func shellCommandExec(dir, command string, getenv func(string) string) (*exec.Cmd, string, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/shell_escape_test.go`:

```go
package tui

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func fakeEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestShellEscapeBin(t *testing.T) {
	cases := []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"linux", map[string]string{"SHELL": "/usr/bin/zsh"}, "/usr/bin/zsh"},
		{"linux", map[string]string{}, "/bin/sh"},
		{"darwin", map[string]string{}, "/bin/sh"},
		{"windows", map[string]string{"COMSPEC": `C:\WINDOWS\system32\cmd.exe`}, `C:\WINDOWS\system32\cmd.exe`},
		{"windows", map[string]string{}, "cmd"},
	}
	for _, c := range cases {
		if got := shellEscapeBin(c.goos, fakeEnv(c.env)); got != c.want {
			t.Errorf("shellEscapeBin(%s, %v) = %q, want %q", c.goos, c.env, got, c.want)
		}
	}
}

func TestSubshellExecPosix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shape")
	}
	cmd, script, err := subshellExec("/some/worktree", fakeEnv(map[string]string{"SHELL": "/usr/bin/zsh"}))
	if err != nil {
		t.Fatalf("subshellExec: %v", err)
	}
	defer os.Remove(script)
	if script == "" {
		t.Fatal("POSIX subshell must use a wrapper script")
	}
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "gg subshell — 'exit' returns to gg") {
		t.Fatalf("script missing the banner:\n%s", body)
	}
	if !strings.Contains(string(body), `exec "${SHELL:-/bin/sh}"`) {
		t.Fatalf("script missing the exec line:\n%s", body)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "/usr/bin/zsh" || cmd.Args[1] != script {
		t.Fatalf("argv = %v, want [/usr/bin/zsh <script>]", cmd.Args)
	}
	if cmd.Dir != "/some/worktree" {
		t.Fatalf("Dir = %q", cmd.Dir)
	}
	found := false
	for _, e := range cmd.Env {
		if e == "GG=1" {
			found = true
		}
	}
	if !found {
		t.Fatal("env must carry GG=1")
	}
}

func TestShellCommandExecPosix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shape")
	}
	cmd, script, err := shellCommandExec("/some/worktree", "git cherry-pick --skip", fakeEnv(nil))
	if err != nil {
		t.Fatalf("shellCommandExec: %v", err)
	}
	defer os.Remove(script)
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "git cherry-pick --skip\n") {
		t.Fatalf("script missing the command:\n%s", s)
	}
	if !strings.Contains(s, "[exit %s] press enter to return to gg") {
		t.Fatalf("script missing the pause line:\n%s", s)
	}
	if !strings.Contains(s, "read -r _ </dev/tty") {
		t.Fatalf("script missing the tty read:\n%s", s)
	}
	if !strings.Contains(s, `exit "$rc"`) {
		t.Fatalf("script must propagate the command's exit code:\n%s", s)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "/bin/sh" || cmd.Args[1] != script {
		t.Fatalf("argv = %v, want [/bin/sh <script>]", cmd.Args)
	}
	if cmd.Dir != "/some/worktree" {
		t.Fatalf("Dir = %q", cmd.Dir)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && go test ./internal/tui/ -run 'TestShellEscapeBin|TestSubshellExec|TestShellCommandExec' 2>&1 | head -8`
Expected: compile FAILURE — `undefined: shellEscapeBin`, `undefined: subshellExec`, `undefined: shellCommandExec`.

- [ ] **Step 3: Implement**

Create `internal/tui/shell_escape.go`:

```go
package tui

import (
	"os"
	"os/exec"
	"runtime"
)

// Shell escape — the emergency hatch (spec: docs/superpowers/specs/
// 2026-07-12-shell-escape-design.md). ctrl+o suspends gg into an interactive
// $SHELL in the worktree; the palette's "Run shell command…" runs one typed
// command with a press-enter-to-return pause. Both ride tea.ExecProcess (the
// $EDITOR / tool_run handover precedent): no engine op, no reservation, no
// approval gate — the user types the command at the moment of execution.

// shellEscapeBin resolves the shell binary: $SHELL (fallback /bin/sh), or on
// Windows %COMSPEC% (fallback cmd). goos and getenv are injected for tests.
func shellEscapeBin(goos string, getenv func(string) string) string {
	if goos == "windows" {
		if c := getenv("COMSPEC"); c != "" {
			return c
		}
		return "cmd"
	}
	if s := getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// shellScriptFile writes body to a temp wrapper script (the toolScript
// mechanics: 0700, .sh/.bat). The caller removes it when the process exits.
func shellScriptFile(body string) (string, error) {
	ext := ".sh"
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	f, err := os.CreateTemp("", "gg-shell-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if err := os.Chmod(f.Name(), 0o700); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// subshellExec builds the interactive-subshell process: banner, then the
// user's shell, cwd = dir. POSIX runs a wrapper script (returned for cleanup);
// Windows needs no script (cmd /K prints the banner and stays interactive).
func subshellExec(dir string, getenv func(string) string) (*exec.Cmd, string, error) {
	bin := shellEscapeBin(runtime.GOOS, getenv)
	var cmd *exec.Cmd
	script := ""
	if runtime.GOOS == "windows" {
		cmd = exec.Command(bin, "/K", "echo gg subshell - 'exit' returns to gg")
	} else {
		body := "echo \"gg subshell — 'exit' returns to gg\"\n" +
			"exec \"${SHELL:-/bin/sh}\"\n"
		var err error
		if script, err = shellScriptFile(body); err != nil {
			return nil, "", err
		}
		cmd = exec.Command(bin, script)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GG=1")
	return cmd, script, nil
}

// shellCommandExec builds the one-off command process: the command runs, then
// a pause keeps its output on screen until the user returns (vim's :! shape).
// The command text is written INTO the script body, never spliced into an
// argv string — no quoting surface beyond what the user themselves typed.
func shellCommandExec(dir, command string, getenv func(string) string) (*exec.Cmd, string, error) {
	bin := shellEscapeBin(runtime.GOOS, getenv)
	var body string
	if runtime.GOOS == "windows" {
		body = command + "\r\n" +
			"set RC=%ERRORLEVEL%\r\n" +
			"echo.\r\n" +
			"echo [exit %RC%] press any key to return to gg\r\n" +
			"pause >nul\r\n" +
			"exit /b %RC%\r\n"
	} else {
		body = command + "\n" +
			"rc=$?\n" +
			"echo\n" +
			"printf '[exit %s] press enter to return to gg' \"$rc\"\n" +
			"read -r _ </dev/tty\n" +
			"exit \"$rc\"\n"
	}
	script, err := shellScriptFile(body)
	if err != nil {
		return nil, "", err
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(bin, "/C", script)
	} else {
		cmd = exec.Command(bin, script)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GG=1")
	return cmd, script, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && go test ./internal/tui/ -run 'TestShellEscapeBin|TestSubshellExec|TestShellCommandExec' -v 2>&1 | tail -8`
Expected: PASS ×3. Also `go build ./...` clean.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape
git add internal/tui/shell_escape.go internal/tui/shell_escape_test.go
git commit -m "feat(tui): shell-escape builders — shell resolution + wrapper scripts

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 2: ctrl+o central key, handover, return path, help

**Files:**
- Modify: `internal/tui/shell_escape.go` (append Model glue)
- Modify: `internal/tui/model.go` (ctrl+o hook + `case shellDoneMsg:`)
- Modify: `internal/tui/help.go` (ctrl+o row)
- Test: `internal/tui/shell_escape_test.go` (append)

**Interfaces:**
- Consumes (Task 1): `subshellExec(dir, getenv)`. Existing: `m.opsIdle()`, `m.currentWorktree`, `m.reloadAllCmd(manual, startup bool)`, `m.anySourceInflight()`, `m.loading`, `tea.ExecProcess`, `conflictProcess{st: confListing}` (test), `footerModel()`/`keyMsg()` (tests).
- Produces (Task 3 relies on): `func (m Model) openSubshell() (Model, tea.Cmd)`, `func (m Model) runShellCommand(command string) (Model, tea.Cmd)`, `type shellDoneMsg struct{ script string; err error }`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/shell_escape_test.go` (add `tea "github.com/charmbracelet/bubbletea"` and `"errors"`, `"os/exec"` to its imports):

```go
func TestCtrlOOpensSubshellFromAnywhere(t *testing.T) {
	// Bare panels.
	m := footerModel()
	mm, cmd := m.Update(keyMsg("ctrl+o"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("ctrl+o on the panels must return the handover cmd")
	}
	// Over an open popup layer (the switcher) — stack untouched.
	m = shellEscTestModelWithLayer()
	mm, cmd = m.Update(keyMsg("ctrl+o"))
	m = mm.(Model)
	if cmd == nil || m.topLayer() == nil {
		t.Fatalf("ctrl+o over a popup must hand over AND keep the stack (cmd=%v top=%v)", cmd, m.topLayer())
	}
	// Over the conflict process — the motivating emergency.
	m = footerModel()
	m.proc = &conflictProcess{st: confListing}
	mm, cmd = m.Update(keyMsg("ctrl+o"))
	m = mm.(Model)
	if cmd == nil || m.proc == nil {
		t.Fatalf("ctrl+o over the conflict process must hand over AND keep the process (cmd=%v proc=%v)", cmd, m.proc)
	}
}

// shellEscTestModelWithLayer builds a model with one popup on the stack.
func shellEscTestModelWithLayer() Model {
	m := footerModel()
	m.width, m.height = 100, 30
	m = m.pushLayer(newShelfPopup(nil))
	return m
}

func TestCtrlOBusyNotices(t *testing.T) {
	m := footerModel()
	m.running = true
	mm, cmd := m.Update(keyMsg("ctrl+o"))
	m = mm.(Model)
	if cmd != nil || !strings.Contains(m.statusMsg, "an operation is running") {
		t.Fatalf("busy ctrl+o must notice, not hand over (cmd=%v msg=%q)", cmd, m.statusMsg)
	}
}

func TestShellDoneReloadsAndCleans(t *testing.T) {
	m := footerModel()
	f, err := os.CreateTemp(t.TempDir(), "gg-shell-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	mm, cmd := m.Update(shellDoneMsg{script: f.Name()})
	m = mm.(Model)
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		t.Fatalf("wrapper script must be removed, stat err=%v", err)
	}
	if m.statusMsg != "returned from shell" || cmd == nil {
		t.Fatalf("return path must set the status and reload (msg=%q cmd=%v)", m.statusMsg, cmd)
	}
}

func TestShellDoneExitErrorIsNotAnError(t *testing.T) {
	m := footerModel()
	exitErr := &exec.ExitError{}
	mm, _ := m.Update(shellDoneMsg{err: exitErr})
	m = mm.(Model)
	if m.statusMsg != "returned from shell" {
		t.Fatalf("a non-zero shell exit is not an error, got %q", m.statusMsg)
	}
	// A genuine launch failure IS an error.
	m = footerModel()
	mm, cmd := m.Update(shellDoneMsg{err: errors.New("fork/exec: no such file")})
	m = mm.(Model)
	if cmd != nil || !strings.HasPrefix(m.statusMsg, "shell: ") {
		t.Fatalf("launch failure must surface (msg=%q)", m.statusMsg)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && go test ./internal/tui/ -run 'TestCtrlO|TestShellDone' 2>&1 | head -8`
Expected: compile FAILURE — `undefined: shellDoneMsg`; after stubbing, behavior failures (ctrl+o unhandled → nil cmd).

- [ ] **Step 3: Implement**

**3a.** Append to `internal/tui/shell_escape.go` (add `"errors"` and `tea "github.com/charmbracelet/bubbletea"` to its imports):

```go
// shellDoneMsg signals the handed-over shell/command process exited. script
// is the wrapper to delete ("" on the Windows subshell, which has none).
type shellDoneMsg struct {
	script string
	err    error
}

// openSubshell suspends gg into an interactive shell in the worktree. Gated
// on opsIdle: the shell must not race a live gg op. (The motivating stuck
// cherry-pick IS idle — the failed continue already returned; the pause
// lives in git's sequencer, not in a running gg op.)
func (m Model) openSubshell() (Model, tea.Cmd) {
	if !m.opsIdle() {
		m.statusMsg = "an operation is running — shell available when it finishes"
		return m, nil
	}
	cmd, script, err := subshellExec(m.currentWorktree, os.Getenv)
	if err != nil {
		m.statusMsg = "shell: " + err.Error()
		return m, nil
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return shellDoneMsg{script: script, err: err}
	})
}

// runShellCommand runs one typed command with the press-enter-to-return
// wrapper. Same gate and return path as the subshell.
func (m Model) runShellCommand(command string) (Model, tea.Cmd) {
	if !m.opsIdle() {
		m.statusMsg = "an operation is running — shell available when it finishes"
		return m, nil
	}
	cmd, script, err := shellCommandExec(m.currentWorktree, command, os.Getenv)
	if err != nil {
		m.statusMsg = "shell: " + err.Error()
		return m, nil
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return shellDoneMsg{script: script, err: err}
	})
}

// handleShellDone is the return path: clean the wrapper, then a full
// manual-grade reload — the user may have committed, skipped, rebased, or
// anything else while gg was suspended. A non-zero EXIT is not an error
// (the subshell inherits the last command's status; the one-off wrapper
// already showed "[exit N]"); only a failure to LAUNCH surfaces.
func (m Model) handleShellDone(msg shellDoneMsg) (Model, tea.Cmd) {
	if msg.script != "" {
		_ = os.Remove(msg.script)
	}
	var exitErr *exec.ExitError
	if msg.err != nil && !errors.As(msg.err, &exitErr) {
		m.statusMsg = "shell: " + msg.err.Error()
		return m, nil
	}
	m.statusMsg = "returned from shell"
	if !m.loading && !m.anySourceInflight() {
		var cmd tea.Cmd
		m, cmd = m.reloadAllCmd(true, false)
		return m, cmd
	}
	return m, nil
}
```

**3b.** `internal/tui/model.go` — two insertions:

1. The central key hook: directly AFTER the `if m.modal != nil { … }` block's closing brace and BEFORE the `// A process owns the interface entirely…` comment (i.e., above `if m.proc != nil`):

```go
		// ctrl+o — the shell escape hatch. Handled ABOVE the process/layer
		// routing (unlike ctrl+p) so it works from ANY surface, including
		// the conflict process and its message screens — the motivating
		// case is a cherry-pick whose continue failed needing --skip. The
		// opsIdle gate lives in openSubshell. A control chord never
		// collides with typed text (the ctrl+t argument).
		if msg.String() == "ctrl+o" {
			return m.openSubshell()
		}
```

2. In `Update`'s message switch (near the other done-msg cases, e.g. next to `case opFinishedMsg:`):

```go
	case shellDoneMsg:
		return m.handleShellDone(msg)
```

**3c.** `internal/tui/help.go` — in the global-keys section, after the `ctrl+t` row:

```go
		r("ctrl+o", "open a shell in the repo worktree (emergency hatch — works over ANY window, incl. a failed conflict resolve; 'exit' returns to gg and reloads)"),
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && go test ./internal/tui/ 2>&1 | tail -3`
Expected: whole package PASS (the new tests plus every pre-existing test — if a help/footer drift-guard flags the new help row, update its fixture as that test's comments instruct).

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape
git add internal/tui/shell_escape.go internal/tui/shell_escape_test.go internal/tui/model.go internal/tui/help.go
git commit -m "feat(tui): ctrl+o shell escape — suspend into \$SHELL from any surface

Handled above the process/layer routing so it reaches the conflict
process's message screens; exit returns with a full reload.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 3: Palette rows + "Run shell command…" popup with history recall

**Files:**
- Modify: `internal/tui/shell_escape.go` (append `shellCmdPopup`)
- Modify: `internal/tui/command_palette.go` (two rows in `paletteCommands()`)
- Modify: `internal/tui/search_history.go` (`scopeShellCmd` const)
- Modify: `internal/tui/help.go` (extend the ctrl+p row's includes-list)
- Test: `internal/tui/shell_escape_test.go` (append)

**Interfaces:**
- Consumes (Task 2): `m.runShellCommand(command)`, `m.openSubshell()`. Existing: `paletteCommand{label, keyHint, run}`, `pushLayer`/`popLayer`, `textfield` (`newTextField`, `HandleEditKey`, `Value`), `viewField`, `popupMax`/`popupResolveWidth`/`popupInnerWidth`/`popupContentWidth`, `modalStyle`, `overlayCenter`/`clipToHeight`, `m.overlayDims()`, `m.recallUpdate(scope, msg, cur)`, `m.recordSearch(scope, phrase)`, `m.recallReset()`, `m.withRecall(frame)`, `m.searchHist`.
- Produces: `func (m Model) openShellCmdPopup() (Model, tea.Cmd)`, `type shellCmdPopup`, `scopeShellCmd = "shellcmd"`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/shell_escape_test.go`:

```go
func TestPaletteHasShellRows(t *testing.T) {
	var labels []string
	for _, c := range paletteCommands() {
		labels = append(labels, c.label)
	}
	joined := strings.Join(labels, "|")
	if !strings.Contains(joined, "Open shell") || !strings.Contains(joined, "Run shell command…") {
		t.Fatalf("palette missing shell rows: %v", labels)
	}
}

func TestShellCmdPopupEnterRunsAndRecords(t *testing.T) {
	m := footerModel()
	m.width, m.height = 100, 30
	mm, _ := m.openShellCmdPopup()
	m = mm

	p := layerOf[*shellCmdPopup](m)
	if p == nil {
		t.Fatal("popup must be on the stack")
	}
	for _, r := range "git cherry-pick --skip" {
		res, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = res.(Model)
	}
	res, cmd := m.Update(keyMsg("enter"))
	m = res.(Model)
	if cmd == nil {
		t.Fatal("enter with a command must dispatch the handover")
	}
	if layerOf[*shellCmdPopup](m) != nil {
		t.Fatal("popup must close on dispatch")
	}
	if len(m.searchHist[scopeShellCmd]) == 0 || m.searchHist[scopeShellCmd][0] != "git cherry-pick --skip" {
		t.Fatalf("command must be recorded for recall, got %v", m.searchHist[scopeShellCmd])
	}
}

func TestShellCmdPopupEmptyEnterNoops(t *testing.T) {
	m := footerModel()
	m.width, m.height = 100, 30
	mm, _ := m.openShellCmdPopup()
	m = mm
	res, cmd := m.Update(keyMsg("enter"))
	m = res.(Model)
	if cmd != nil || layerOf[*shellCmdPopup](m) == nil {
		t.Fatal("empty enter must keep the popup open and dispatch nothing")
	}
	res, _ = m.Update(keyMsg("esc"))
	m = res.(Model)
	if layerOf[*shellCmdPopup](m) != nil {
		t.Fatal("esc must close the popup")
	}
}

func TestShellCmdPopupRecall(t *testing.T) {
	m := footerModel()
	m.width, m.height = 100, 30
	m.searchHist = map[string][]string{scopeShellCmd: {"git status"}}
	mm, _ := m.openShellCmdPopup()
	m = mm
	res, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m = res.(Model)
	if p := layerOf[*shellCmdPopup](m); p == nil || p.input.Value() != "git status" {
		t.Fatalf("alt+down must preview history into the field, got %q", p.input.Value())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && go test ./internal/tui/ -run 'TestPaletteHasShellRows|TestShellCmdPopup' 2>&1 | head -8`
Expected: compile FAILURE — `undefined: shellCmdPopup`, `m.openShellCmdPopup undefined`, `undefined: scopeShellCmd`.

- [ ] **Step 3: Implement**

**3a.** `internal/tui/search_history.go` — add to the scopes const block:

```go
	scopeShellCmd = "shellcmd"
```

**3b.** Append to `internal/tui/shell_escape.go` (add `"strings"` to imports):

```go
// shellCmdPopup collects one shell command to run with the press-enter-to-
// return wrapper (the repoPathPopup pattern). alt+↓/↑ recalls previous
// commands via the shared search-history rings.
type shellCmdPopup struct {
	popupMax
	input textfield
}

func (m Model) openShellCmdPopup() (Model, tea.Cmd) {
	return m.pushLayer(&shellCmdPopup{input: newTextField("")}), nil
}

func (p *shellCmdPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// History recall: a committed pick (enter on the dropdown) only fills the
	// field — the user presses enter again to actually run it.
	if nm, nq, handled, _ := m.recallUpdate(scopeShellCmd, msg, p.input.Value()); handled {
		p.input = newTextField(nq)
		return nm, nil
	} else {
		m = nm
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.recallReset().popLayer(), nil
	case tea.KeyEnter:
		command := strings.TrimSpace(p.input.Value())
		if command == "" {
			return m, nil // nothing to run; keep the popup open
		}
		var record tea.Cmd
		m, record = m.recordSearch(scopeShellCmd, command)
		m = m.popLayer()
		var run tea.Cmd
		m, run = m.runShellCommand(command)
		return m, tea.Batch(record, run)
	default:
		p.input.HandleEditKey(msg) // spaces included — do NOT swallow KeySpace
	}
	return m, nil
}

func (p *shellCmdPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString("Run a shell command in the worktree\n\n")
	b.WriteString(viewField("$ ", p.input, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[enter] run  [alt+↓] history  [esc] cancel")
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return m.withRecall(overlayCenter(clipToHeight(below, h), box, w, h))
}
```

NOTE: check `recallReset()`'s return type at the call site — it returns `Model`, so `m.recallReset().popLayer()` chains; if the chain reads awkwardly against local style, split into two statements.

**3c.** `internal/tui/command_palette.go` — insert into `paletteCommands()` keeping the alphabetical order (between "Git config explorer" and "Open repo", and between "Open repo" and "Set up agent skills"):

```go
		{label: "Open shell", keyHint: "ctrl+o", run: func(m Model) (Model, tea.Cmd) { m = m.popLayer(); return m.openSubshell() }},
```

```go
		{label: "Run shell command…", run: func(m Model) (Model, tea.Cmd) { return m.openShellCmdPopup() }},
```

(The run-command popup is pushed OVER the palette — esc returns to it, the "Open repo" convention; "Open shell" pops the palette first so the handover leaves a clean stack behind, and on return the panels reload.)

**3d.** `internal/tui/help.go` — the existing `ctrl+p` row lists the palette's entries; extend its includes-list to stay accurate:
`…includes Apply patch, File blame, File history, Find, Git config explorer, Open repo, Open shell, Run shell command, Set up agent skills, Show commit`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && go test ./internal/tui/ 2>&1 | tail -3`
Expected: whole package PASS.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape
git add internal/tui/shell_escape.go internal/tui/shell_escape_test.go internal/tui/command_palette.go internal/tui/search_history.go internal/tui/help.go
git commit -m "feat(tui): palette Open shell + Run shell command… with history recall

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

---

### Task 4: Full verification + docs

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-07-12-shell-escape-design.md` (Status line)

**Interfaces:** none — verification and documentation.

- [ ] **Step 1: Run the full staged suite**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && ./test.sh 2>&1 | tail -8` (timeout 600000 ms)
Expected: vet+gofmt clean, unit ok, e2e ok, "all green".

- [ ] **Step 2: Run with the race detector**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && ./test.sh race 2>&1 | tail -6` (timeout 600000 ms)
Expected: "all green".

- [ ] **Step 3: Update the docs**

- `CHANGELOG.md`, under the Unreleased "Added" heading (match the file's entry style):

```markdown
- Shell escape: `ctrl+o` anywhere in the TUI (even over a failed conflict
  resolve) suspends gg into an interactive `$SHELL` in the worktree — run
  whatever git needs (`git cherry-pick --skip`, …), `exit` returns to gg
  with a full reload. The `ctrl+p` palette gains **Open shell** and **Run
  shell command…** (one-off command with a press-enter-to-return pause and
  `alt+↓` history recall).
```

- `README.md`: add `ctrl+o` to the global-keys documentation and the two rows to the palette list, matching the surrounding phrasing.
- `CLAUDE.md`: in the `tui` package-map row, append a terse clause: shell escape (`shell_escape.go`: `ctrl+o` handled above the process/layer routing → interactive `$SHELL`/`%COMSPEC%` subshell via `tea.ExecProcess`, cwd = worktree, `GG=1`; palette "Run shell command…" one-off with pause + `scopeShellCmd` recall; return path = full reload; non-zero exit ≠ error, `$EDITOR` standing, no approval gate).
- Spec: flip `**Status:**` to `implemented on feat/shell-escape`.

- [ ] **Step 4: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape
git add CHANGELOG.md README.md CLAUDE.md docs/superpowers/specs/2026-07-12-shell-escape-design.md
git commit -m "docs: shell escape (ctrl+o subshell + palette run-command)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CpRmKAbmQKQAKjHXv82aJ9"
```

- [ ] **Step 5: Build the binary and report**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/shell-escape && go build -o ./gg ./cmd/gg && ./gg version`
Expected: binary built and runs. The controller delivers it (SendUserFile, absolute path). Do NOT merge — the human owns the trunk.

---

## Manual verification (controller, after Task 4)

The terminal handover itself cannot be unit-tested (the `$EDITOR` precedent). Before offering the merge, manually verify in a real terminal if feasible, or ask the user to: (1) `ctrl+o` from the panels → banner, prompt, `exit` → gg reloads; (2) palette → Run shell command… → `git status` → output + pause → enter → back.
