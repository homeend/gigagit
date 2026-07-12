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
