package tui

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
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
//
// The POSIX wrapper is sh syntax, so it is interpreted by /bin/sh regardless
// of $SHELL (a fish or csh user would otherwise hit a parse error and the
// escape hatch would break entirely) — the handoff to the user's own shell
// happens INSIDE the script via `exec "${SHELL:-/bin/sh}"`, which re-reads
// $SHELL from the child's environment, so it still lands the user in fish/csh
// as expected.
func subshellExec(dir string, getenv func(string) string) (*exec.Cmd, string, error) {
	var cmd *exec.Cmd
	script := ""
	if runtime.GOOS == "windows" {
		bin := shellEscapeBin(runtime.GOOS, getenv)
		cmd = exec.Command(bin, "/K", "echo gg subshell - 'exit' returns to gg")
	} else {
		body := "echo \"gg subshell — 'exit' returns to gg\"\n" +
			"exec \"${SHELL:-/bin/sh}\"\n"
		var err error
		if script, err = shellScriptFile(body); err != nil {
			return nil, "", err
		}
		cmd = exec.Command("/bin/sh", script)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GG=1")
	return cmd, script, nil
}

// shellCommandExec builds the one-off command process: the command runs, then
// a pause keeps its output on screen until the user returns (vim's :! shape).
// The command text is written INTO the script body, never spliced into an
// argv string — no quoting surface beyond what the user themselves typed.
// The POSIX wrapper is sh syntax (like subshellExec's), so it runs under
// /bin/sh unconditionally rather than $SHELL — the one-off command itself
// deliberately gets sh semantics too (the lazygit convention: `:!`-style
// one-liners run under sh, not the user's interactive shell).
func shellCommandExec(dir, command string, getenv func(string) string) (*exec.Cmd, string, error) {
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
		bin := shellEscapeBin(runtime.GOOS, getenv)
		cmd = exec.Command(bin, "/C", script)
	} else {
		cmd = exec.Command("/bin/sh", script)
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GG=1")
	return cmd, script, nil
}

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
		m.statusMsg = i18n.T("an operation is running — shell available when it finishes")
		return m, nil
	}
	cmd, script, err := subshellExec(m.currentWorktree, os.Getenv)
	if err != nil {
		m.statusMsg = i18n.T("shell: %s", err.Error())
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
		m.statusMsg = i18n.T("an operation is running — shell available when it finishes")
		return m, nil
	}
	cmd, script, err := shellCommandExec(m.currentWorktree, command, os.Getenv)
	if err != nil {
		m.statusMsg = i18n.T("shell: %s", err.Error())
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
		m.statusMsg = i18n.T("shell: %s", msg.err.Error())
		return m, nil
	}
	m.statusMsg = i18n.T("returned from shell")
	// Always reload, even mid an in-flight read: the user may have committed,
	// rebased, or anything else while gg was suspended, and each source's
	// per-source gen bump (reloadSourcesCmd) makes this safe unconditionally —
	// any stale in-flight read is dropped when it lands.
	var cmd tea.Cmd
	// hardFeed: arbitrary git (rebase, reset, filter-branch) may have run in the
	// shell, so start the commit list clean rather than trust the accumulation.
	m, cmd = m.reloadAllCmd(reloadOpts{manual: true, hardFeed: true})
	return m, cmd
}

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
		m = m.recallReset()
		return m.popLayer(), nil
	case tea.KeyEnter:
		command := strings.TrimSpace(p.input.Value())
		if command == "" {
			return m, nil // nothing to run; keep the popup open
		}
		var record tea.Cmd
		m, record = m.recordSearch(scopeShellCmd, command)
		m = m.popLayer() // the command popup
		if _, ok := m.topLayer().(*commandPalette); ok {
			m = m.popLayer() // the palette that launched it
		}
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
	b.WriteString(i18n.T("Run a shell command in the worktree") + "\n\n")
	b.WriteString(viewField("$ ", p.input, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(i18n.T("[enter] run  [alt+↓] history  [esc] cancel"))
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
