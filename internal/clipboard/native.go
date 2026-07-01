package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"
)

// ErrUnavailable means no clipboard method succeeded: no native command was
// found and no tty was available for the OSC 52 fallback.
var ErrUnavailable = errors.New("clipboard: no clipboard method available")

// nativeArgv returns the argv for the platform's clipboard-copy command, or
// ok=false when none is on PATH. It is pure: GOOS, the WSL signal, the
// environment, and PATH lookup are all injected so the detection matrix is
// unit-testable. The command reads the text to copy from its stdin.
//
// Ordering matters on Linux: under WSL, clip.exe wins over wl-copy/xclip even
// when WSLg makes those present, because the Windows clipboard is the one the
// user actually sees. Off WSL, Wayland's wl-copy is preferred when a Wayland
// display is set, then the X11 tools.
func nativeArgv(goos string, isWSL bool, env func(string) string, lookPath func(string) (string, error)) ([]string, bool) {
	has := func(name string) bool { _, err := lookPath(name); return err == nil }

	switch goos {
	case "darwin":
		if has("pbcopy") {
			return []string{"pbcopy"}, true
		}
	case "windows":
		if has("clip") {
			return []string{"clip"}, true
		}
	case "linux":
		if isWSL && has("clip.exe") {
			return []string{"clip.exe"}, true
		}
		if env("WAYLAND_DISPLAY") != "" && has("wl-copy") {
			return []string{"wl-copy"}, true
		}
		if has("xclip") {
			return []string{"xclip", "-selection", "clipboard"}, true
		}
		if has("xsel") {
			return []string{"xsel", "--clipboard", "--input"}, true
		}
	}
	return nil, false
}

// detectWSL reports whether we run under WSL. It reads the kernel osrelease
// rather than $WSL_DISTRO_NAME, because tmux captures its environment at
// server start and does not refresh that variable on attach — a tmux server
// started without it would otherwise hide WSL and silently drop us to OSC 52.
func detectWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	s := strings.ToLower(string(b))
	return strings.Contains(s, "microsoft") || strings.Contains(s, "wsl")
}

// preferOSC52 reports whether the OSC 52 escape should be tried before the
// native command. Inside an SSH session the native clipboard tool would set
// the remote machine's clipboard, not the one in front of the user; OSC 52
// travels back to the local terminal, so it wins there.
func preferOSC52(env func(string) string) bool {
	return env("SSH_TTY") != "" || env("SSH_CONNECTION") != ""
}

// clipboardStdin returns the bytes to feed the native command's stdin.
// Windows' clip.exe (invoked as "clip.exe" under WSL or "clip" natively — the
// same binary either way) guesses whether stdin is already UTF-16 using a
// length-sensitive heuristic; short pure-ASCII payloads such as a git tag
// name ("v0.1.9") are frequently misdetected as UTF-16 and stored verbatim,
// which then reads back as mojibake in the CJK range (each ASCII byte pair
// reinterpreted as one UTF-16 code unit). A 40-char SHA carries enough
// signal to be detected correctly, which is why only short copies like tag
// names show the bug. Encoding explicitly to UTF-16LE removes the ambiguity
// the heuristic acts on. Every other native command reads UTF-8 as-is.
func clipboardStdin(cmdName, text string) []byte {
	if cmdName != "clip.exe" && cmdName != "clip" {
		return []byte(text)
	}
	units := utf16.Encode([]rune(text))
	buf := make([]byte, 0, len(units)*2)
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return buf
}

// runArgv pipes text to the stdin of argv[0] with argv[1:] as arguments. It is
// a package var so tests can substitute a fake without spawning a process (and
// without clobbering the developer's real clipboard).
var runArgv = func(argv []string, text string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(clipboardStdin(argv[0], text))
	return cmd.Run()
}

// sysClipboard captures the resolved environment for a single copy so the
// ordering logic can be exercised in tests without touching the real system.
type sysClipboard struct {
	argv      []string                     // native command argv; nil when none available
	run       func([]string, string) error // executes the native command
	preferOSC bool                         // try OSC 52 before native (SSH)
}

// copy writes text to the clipboard, trying native and OSC 52 in the order set
// by preferOSC. tty may be nil when no terminal is available (OSC 52 skipped).
// It returns a short method label for status reporting.
func (c sysClipboard) copy(tty io.Writer, text string) (string, error) {
	tryNative := func() (string, bool, error) {
		if c.argv == nil {
			return "", false, nil
		}
		if err := c.run(c.argv, text); err != nil {
			return "", false, fmt.Errorf("clipboard: %s: %w", c.argv[0], err)
		}
		return c.argv[0], true, nil
	}
	tryOSC52 := func() (string, bool, error) {
		if tty == nil {
			return "", false, nil
		}
		if err := writeOSC52(tty, text); err != nil {
			return "", false, err
		}
		return "osc52", true, nil
	}

	order := []func() (string, bool, error){tryNative, tryOSC52}
	if c.preferOSC {
		order[0], order[1] = order[1], order[0]
	}

	var firstErr error
	for _, try := range order {
		name, done, err := try()
		if done {
			return name, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return "", firstErr
	}
	return "", ErrUnavailable
}

// Copy writes text to the system clipboard using the best available method:
// the platform's native clipboard command for local sessions, the OSC 52
// escape written to tty for remote/SSH sessions or as a fallback. tty may be
// nil when no terminal is available. It returns a short method label
// ("clip.exe", "osc52", …) for status reporting.
func Copy(tty io.Writer, text string) (string, error) {
	argv, _ := nativeArgv(runtime.GOOS, detectWSL(), os.Getenv, exec.LookPath)
	c := sysClipboard{argv: argv, run: runArgv, preferOSC: preferOSC52(os.Getenv)}
	return c.copy(tty, text)
}
