package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// ErrUnavailable means no clipboard method succeeded: no native command was
// found and no tty was available for the OSC 52 fallback.
var ErrUnavailable = errors.New("clipboard: no clipboard method available")

// nativeCopy describes how to run the platform's native clipboard command:
// the argv plus any extra environment the subprocess needs (KEY=VALUE). env is
// nil for every command except a wl-copy whose WAYLAND_DISPLAY had to be
// recovered off-environment (see nativeCopyCmd's Wayland branch).
type nativeCopy struct {
	argv []string
	env  []string
}

// nativeCopyCmd returns how to run the platform's clipboard-copy command, or
// ok=false when none is usable. It is pure: GOOS, the WSL signal, the
// environment, PATH lookup, and Wayland-display resolution are all injected so
// the detection matrix is unit-testable. The command reads the text to copy
// from its stdin.
//
// Ordering matters on Linux: under WSL, clip.exe wins over wl-copy/xclip even
// when WSLg makes those present, because the Windows clipboard is the one the
// user actually sees. Off WSL, Wayland's wl-copy is preferred when a Wayland
// display is resolvable, then the X11 tools.
//
// wl-copy needs a WAYLAND_DISPLAY to connect. It is gated on `waylandDisplay`
// resolving one rather than on `env("WAYLAND_DISPLAY") != ""`, because tmux
// does NOT propagate WAYLAND_DISPLAY into its environment (it is not in tmux's
// default update-environment set) — so inside tmux the var is empty even on a
// live Wayland session. `waylandDisplay` recovers it by probing the runtime
// dir; when it comes back off-environment we inject it so the wl-copy child
// can connect. This mirrors detectWSL's reason for reading osrelease instead
// of $WSL_DISTRO_NAME: the same tmux env-staleness, a different variable.
func nativeCopyCmd(goos string, isWSL bool, env func(string) string, lookPath func(string) (string, error), waylandDisplay func() (string, bool)) (nativeCopy, bool) {
	has := func(name string) bool { _, err := lookPath(name); return err == nil }

	switch goos {
	case "darwin":
		if has("pbcopy") {
			return nativeCopy{argv: []string{"pbcopy"}}, true
		}
	case "windows":
		if has("clip") {
			return nativeCopy{argv: []string{"clip"}}, true
		}
	case "linux":
		if isWSL && has("clip.exe") {
			return nativeCopy{argv: []string{"clip.exe"}}, true
		}
		if has("wl-copy") {
			if disp, ok := waylandDisplay(); ok {
				nc := nativeCopy{argv: []string{"wl-copy"}}
				if env("WAYLAND_DISPLAY") == "" {
					// Recovered off-env (tmux stripped it): the child needs it.
					nc.env = []string{"WAYLAND_DISPLAY=" + disp}
				}
				return nc, true
			}
		}
		if has("xclip") {
			return nativeCopy{argv: []string{"xclip", "-selection", "clipboard"}}, true
		}
		if has("xsel") {
			return nativeCopy{argv: []string{"xsel", "--clipboard", "--input"}}, true
		}
	}
	return nativeCopy{}, false
}

// resolveWaylandDisplay returns the WAYLAND_DISPLAY to use for wl-copy. A value
// already in the environment is used verbatim; otherwise (the tmux case) the
// runtime dir is scanned for a live wayland-N socket and its absolute path is
// returned, so the wl-copy child connects even without XDG_RUNTIME_DIR set.
func resolveWaylandDisplay(env func(string) string) (string, bool) {
	if d := env("WAYLAND_DISPLAY"); d != "" {
		return d, true
	}
	runtimeDir := env("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return findWaylandSocket(runtimeDir)
}

// findWaylandSocket scans runtimeDir for a Wayland display socket (wayland-0,
// wayland-1, …), skipping the sibling ".lock" regular file, and returns the
// absolute path of the lowest-named one. Returning an absolute path (not a bare
// name) means WAYLAND_DISPLAY works even if XDG_RUNTIME_DIR is absent from the
// child's environment — libwayland treats an absolute WAYLAND_DISPLAY as-is.
func findWaylandSocket(runtimeDir string) (string, bool) {
	entries, err := os.ReadDir(runtimeDir)
	if err != nil {
		return "", false
	}
	best := ""
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "wayland-") || strings.HasSuffix(name, ".lock") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			continue
		}
		if best == "" || name < best {
			best = name
		}
	}
	if best == "" {
		return "", false
	}
	return filepath.Join(runtimeDir, best), true
}

// Availability reports whether gg can put text on the system clipboard via a
// native command, and — when it cannot — the local display session that was
// detected and what to install for it. It backs the "install a clipboard tool"
// notice: a CLI app cannot set the X11/Wayland clipboard from raw bytes, it
// needs xclip/xsel or wl-copy; without one, gg is stuck on the OSC 52 terminal
// escape, which many terminals (and tmux without extra config) do not honour.
type Availability struct {
	Available bool   // a native clipboard command is present and usable
	Tool      string // the command gg will use, when Available (e.g. "xclip")
	Session   string // detected local display: "wayland", "x11", or "" (headless/unknown)
	Install   string // package to install when a present local display lacks its tool ("" = nothing to suggest)
}

// probe is the pure core of Probe: it resolves what Copy would do from injected
// GOOS/WSL/env/PATH/Wayland deps. Install is set ONLY when a local display is
// present but its native tool is missing — the unambiguous "there is a
// clipboard right here and gg can't reach it" case. A headless/SSH session
// (no local display) leaves Install empty: OSC 52 is the expected path there
// and a "missing tool" nag would be a false positive.
func probe(goos string, isWSL bool, env func(string) string, lookPath func(string) (string, error), waylandDisplay func() (string, bool)) Availability {
	disp, wlOK := "", false
	if goos == "linux" {
		disp, wlOK = waylandDisplay()
	}
	memoWayland := func() (string, bool) { return disp, wlOK }

	var av Availability
	switch {
	case wlOK:
		av.Session = "wayland"
	case goos == "linux" && env("DISPLAY") != "":
		av.Session = "x11"
	}

	if nc, ok := nativeCopyCmd(goos, isWSL, env, lookPath, memoWayland); ok {
		av.Available = true
		av.Tool = nc.argv[0]
		return av
	}
	switch av.Session {
	case "wayland":
		av.Install = "wl-clipboard"
	case "x11":
		av.Install = "xclip"
	}
	return av
}

// Probe reports whether gg has a native clipboard command and, if not, what to
// install for the detected local display. It runs the same detection as Copy
// (including the tmux-safe Wayland-socket recovery), so a notice built from it
// matches what Copy will actually do.
func Probe() Availability {
	return probe(runtime.GOOS, detectWSL(), os.Getenv, exec.LookPath,
		func() (string, bool) { return resolveWaylandDisplay(os.Getenv) })
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

// runArgv pipes text to the stdin of argv[0] with argv[1:] as arguments,
// adding extraEnv (KEY=VALUE) to the inherited environment when non-empty (a
// recovered WAYLAND_DISPLAY for wl-copy). It is a package var so tests can
// substitute a fake without spawning a process (and without clobbering the
// developer's real clipboard).
var runArgv = func(argv []string, extraEnv []string, text string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(clipboardStdin(argv[0], text))
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	return cmd.Run()
}

// sysClipboard captures the resolved environment for a single copy so the
// ordering logic can be exercised in tests without touching the real system.
type sysClipboard struct {
	argv      []string                                 // native command argv; nil when none available
	argvEnv   []string                                 // extra env for the native argv (recovered WAYLAND_DISPLAY); may be nil
	run       func(argv, env []string, s string) error // executes the native command
	preferOSC bool                                     // try OSC 52 before native (SSH)
}

// copy writes text to the clipboard, trying native and OSC 52 in the order set
// by preferOSC. tty may be nil when no terminal is available (OSC 52 skipped).
// It returns a short method label for status reporting.
func (c sysClipboard) copy(tty io.Writer, text string) (string, error) {
	tryNative := func() (string, bool, error) {
		if c.argv == nil {
			return "", false, nil
		}
		if err := c.run(c.argv, c.argvEnv, text); err != nil {
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
	nc, _ := nativeCopyCmd(runtime.GOOS, detectWSL(), os.Getenv, exec.LookPath,
		func() (string, bool) { return resolveWaylandDisplay(os.Getenv) })
	c := sysClipboard{argv: nc.argv, argvEnv: nc.env, run: runArgv, preferOSC: preferOSC52(os.Getenv)}
	return c.copy(tty, text)
}
