package clipboard

import (
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// lookOnly returns a lookPath stub that "finds" exactly the named commands.
func lookOnly(names ...string) func(string) (string, error) {
	set := map[string]bool{}
	for _, n := range names {
		set[n] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func noEnv(string) string { return "" }

func envWith(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

// noWayland / waylandAt are waylandDisplay stubs for nativeCopyCmd.
func noWayland() (string, bool) { return "", false }
func waylandAt(disp string) func() (string, bool) {
	return func() (string, bool) { return disp, true }
}

func TestNativeCopyCmd(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		isWSL   bool
		env     func(string) string
		look    func(string) (string, error)
		wayland func() (string, bool)
		want    nativeCopy
		wantOK  bool
	}{
		{
			name: "macOS pbcopy", goos: "darwin", env: noEnv,
			look: lookOnly("pbcopy"), wayland: noWayland,
			want: nativeCopy{argv: []string{"pbcopy"}}, wantOK: true,
		},
		{
			name: "windows clip", goos: "windows", env: noEnv,
			look: lookOnly("clip"), wayland: noWayland,
			want: nativeCopy{argv: []string{"clip"}}, wantOK: true,
		},
		{
			name: "WSL prefers clip.exe over wl-copy", goos: "linux", isWSL: true,
			env:     envWith(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
			look:    lookOnly("clip.exe", "wl-copy"),
			wayland: waylandAt("wayland-0"),
			want:    nativeCopy{argv: []string{"clip.exe"}}, wantOK: true,
		},
		{
			name: "Linux Wayland wl-copy, display already in env (no inject)", goos: "linux",
			env:     envWith(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
			look:    lookOnly("wl-copy", "xclip"),
			wayland: waylandAt("wayland-0"),
			want:    nativeCopy{argv: []string{"wl-copy"}}, wantOK: true,
		},
		{
			// The reported bug: Wayland session inside tmux, WAYLAND_DISPLAY
			// stripped by tmux, but the socket is discoverable — wl-copy must
			// be chosen and the display injected into its environment.
			name: "Linux Wayland wl-copy, display discovered off-env (inject)", goos: "linux",
			env:     noEnv, // WAYLAND_DISPLAY unset (as inside tmux)
			look:    lookOnly("wl-copy"),
			wayland: waylandAt("/run/user/1000/wayland-0"),
			want: nativeCopy{
				argv: []string{"wl-copy"},
				env:  []string{"WAYLAND_DISPLAY=/run/user/1000/wayland-0"},
			}, wantOK: true,
		},
		{
			name: "Linux X11 xclip", goos: "linux", env: noEnv,
			look: lookOnly("xclip"), wayland: noWayland,
			want: nativeCopy{argv: []string{"xclip", "-selection", "clipboard"}}, wantOK: true,
		},
		{
			name: "Linux xsel fallback", goos: "linux", env: noEnv,
			look: lookOnly("xsel"), wayland: noWayland,
			want: nativeCopy{argv: []string{"xsel", "--clipboard", "--input"}}, wantOK: true,
		},
		{
			name: "Linux no tool", goos: "linux", env: noEnv,
			look: lookOnly(), wayland: noWayland,
			want: nativeCopy{}, wantOK: false,
		},
		{
			// wl-copy present but no Wayland at all (pure X11, no socket): must
			// NOT pick wl-copy (it would fail), and with no X11 tool, give up.
			name: "Linux wl-copy present but no Wayland → none", goos: "linux",
			env:     noEnv,
			look:    lookOnly("wl-copy"),
			wayland: noWayland,
			want:    nativeCopy{}, wantOK: false,
		},
		{
			name: "WSL but clip.exe missing falls to xclip", goos: "linux", isWSL: true, env: noEnv,
			look: lookOnly("xclip"), wayland: noWayland,
			want: nativeCopy{argv: []string{"xclip", "-selection", "clipboard"}}, wantOK: true,
		},
		{
			name: "wl-copy missing, Wayland var set, falls to xclip", goos: "linux",
			env:     envWith(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
			look:    lookOnly("xclip"),
			wayland: waylandAt("wayland-0"),
			want:    nativeCopy{argv: []string{"xclip", "-selection", "clipboard"}}, wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := nativeCopyCmd(tt.goos, tt.isWSL, tt.env, tt.look, tt.wayland)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (cmd=%+v)", ok, tt.wantOK, got)
			}
			if strings.Join(got.argv, " ") != strings.Join(tt.want.argv, " ") {
				t.Errorf("argv = %v, want %v", got.argv, tt.want.argv)
			}
			if strings.Join(got.env, " ") != strings.Join(tt.want.env, " ") {
				t.Errorf("env = %v, want %v", got.env, tt.want.env)
			}
		})
	}
}

// TestResolveWaylandDisplayFromEnv: a set WAYLAND_DISPLAY is used verbatim,
// with no filesystem probe.
func TestResolveWaylandDisplayFromEnv(t *testing.T) {
	disp, ok := resolveWaylandDisplay(envWith(map[string]string{"WAYLAND_DISPLAY": "wayland-3"}))
	if !ok || disp != "wayland-3" {
		t.Errorf("resolveWaylandDisplay = (%q, %v), want (wayland-3, true)", disp, ok)
	}
}

// TestFindWaylandSocket verifies the $XDG_RUNTIME_DIR scan that recovers the
// display tmux strips: it returns the absolute path of a real wayland-N socket,
// ignores the sibling .lock file, and reports false for an empty dir.
func TestFindWaylandSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket probe is a POSIX concern")
	}
	dir := t.TempDir()

	if _, ok := findWaylandSocket(dir); ok {
		t.Fatalf("empty runtime dir must not yield a socket")
	}

	// A regular file named like the lock must be ignored.
	if err := os.WriteFile(filepath.Join(dir, "wayland-1.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A real listening unix socket named wayland-1.
	sockPath := filepath.Join(dir, "wayland-1")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer ln.Close()

	got, ok := findWaylandSocket(dir)
	if !ok || got != sockPath {
		t.Errorf("findWaylandSocket = (%q, %v), want (%q, true)", got, ok, sockPath)
	}
}

// TestClipboardStdinEncodesUTF16LEForClipExe guards against a real bug:
// clip.exe's stdin-encoding heuristic misdetects short ASCII payloads (e.g.
// git tag names) as already being UTF-16 and stores them verbatim, which
// then pastes as CJK-range mojibake. Verified by piping "v0.1.9" through
// clip.exe on WSL and reading it back with Get-Clipboard: it came back as
// "ぶㄮ㤮" — exactly what you get by reinterpreting these UTF-16LE bytes as
// UTF-8. Encoding to UTF-16LE up front removes the ambiguity.
func TestClipboardStdinEncodesUTF16LEForClipExe(t *testing.T) {
	for _, cmdName := range []string{"clip.exe", "clip"} {
		got := clipboardStdin(cmdName, "v0.1.9")
		want := []byte{'v', 0, '0', 0, '.', 0, '1', 0, '.', 0, '9', 0}
		if !bytes.Equal(got, want) {
			t.Errorf("clipboardStdin(%q) = %v, want %v", cmdName, got, want)
		}
	}
}

func TestClipboardStdinLeavesOtherCommandsAsUTF8(t *testing.T) {
	for _, cmdName := range []string{"pbcopy", "wl-copy", "xclip", "xsel"} {
		got := clipboardStdin(cmdName, "v0.1.9 café 🚀")
		if string(got) != "v0.1.9 café 🚀" {
			t.Errorf("clipboardStdin(%q) = %q, want unchanged UTF-8 text", cmdName, got)
		}
	}
}

// TestProbe covers the missing-tool detection behind the notification: a local
// display without its native tool yields an Install hint; a present tool or a
// headless session does not.
func TestProbe(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		isWSL       bool
		env         func(string) string
		look        func(string) (string, error)
		wayland     func() (string, bool)
		wantAvail   bool
		wantTool    string
		wantSession string
		wantInstall string
	}{
		{
			name: "X11 desktop, no clipboard tool → suggest xclip", goos: "linux",
			env: envWith(map[string]string{"DISPLAY": ":0"}),
			// wl-copy present but no Wayland socket (the reported machine).
			look: lookOnly("wl-copy"), wayland: noWayland,
			wantAvail: false, wantSession: "x11", wantInstall: "xclip",
		},
		{
			name: "X11 desktop with xclip → available", goos: "linux",
			env:  envWith(map[string]string{"DISPLAY": ":0"}),
			look: lookOnly("xclip"), wayland: noWayland,
			wantAvail: true, wantTool: "xclip", wantSession: "x11",
		},
		{
			name: "Wayland session, no wl-copy → suggest wl-clipboard", goos: "linux",
			env: noEnv, look: lookOnly(), wayland: waylandAt("/run/user/1000/wayland-0"),
			wantAvail: false, wantSession: "wayland", wantInstall: "wl-clipboard",
		},
		{
			name: "Wayland session with wl-copy → available", goos: "linux",
			env: noEnv, look: lookOnly("wl-copy"), wayland: waylandAt("/run/user/1000/wayland-0"),
			wantAvail: true, wantTool: "wl-copy", wantSession: "wayland",
		},
		{
			name: "headless: no display, no tool → no Install (OSC 52 territory)", goos: "linux",
			env: noEnv, look: lookOnly(), wayland: noWayland,
			wantAvail: false, wantSession: "", wantInstall: "",
		},
		{
			name: "macOS pbcopy → available", goos: "darwin",
			env: noEnv, look: lookOnly("pbcopy"), wayland: noWayland,
			wantAvail: true, wantTool: "pbcopy",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			av := probe(tt.goos, tt.isWSL, tt.env, tt.look, tt.wayland)
			if av.Available != tt.wantAvail {
				t.Errorf("Available = %v, want %v", av.Available, tt.wantAvail)
			}
			if av.Tool != tt.wantTool {
				t.Errorf("Tool = %q, want %q", av.Tool, tt.wantTool)
			}
			if av.Session != tt.wantSession {
				t.Errorf("Session = %q, want %q", av.Session, tt.wantSession)
			}
			if av.Install != tt.wantInstall {
				t.Errorf("Install = %q, want %q", av.Install, tt.wantInstall)
			}
		})
	}
}

func TestPreferOSC52(t *testing.T) {
	if preferOSC52(noEnv) {
		t.Error("local session should not prefer OSC 52")
	}
	if !preferOSC52(envWith(map[string]string{"SSH_TTY": "/dev/pts/0"})) {
		t.Error("SSH_TTY set should prefer OSC 52")
	}
	if !preferOSC52(envWith(map[string]string{"SSH_CONNECTION": "1.2.3.4 1 5.6.7.8 22"})) {
		t.Error("SSH_CONNECTION set should prefer OSC 52")
	}
}

// recordRun captures the argv/env/text passed to the native command without
// spawning a process or touching the real clipboard.
type recordRun struct {
	called bool
	argv   []string
	env    []string
	text   string
	err    error
}

func (r *recordRun) run(argv, env []string, text string) error {
	r.called = true
	r.argv = argv
	r.env = env
	r.text = text
	return r.err
}

func TestCopyLocalUsesNativeFirst(t *testing.T) {
	var rec recordRun
	var tty countWriter
	c := sysClipboard{argv: []string{"clip.exe"}, run: rec.run, preferOSC: false}
	method, err := c.copy(&tty, "café 🚀")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !rec.called || rec.text != "café 🚀" {
		t.Errorf("native run not invoked with text; called=%v text=%q", rec.called, rec.text)
	}
	if method != "clip.exe" {
		t.Errorf("method = %q, want clip.exe", method)
	}
	if tty.n != 0 {
		t.Errorf("OSC 52 must not be written when native succeeds (writes=%d)", tty.n)
	}
}

// TestCopyPassesNativeEnv: the injected WAYLAND_DISPLAY (recovered off-env for
// a tmux session) must reach the wl-copy subprocess.
func TestCopyPassesNativeEnv(t *testing.T) {
	var rec recordRun
	c := sysClipboard{
		argv:    []string{"wl-copy"},
		argvEnv: []string{"WAYLAND_DISPLAY=/run/user/1000/wayland-0"},
		run:     rec.run, preferOSC: false,
	}
	if _, err := c.copy(nil, "hi"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if strings.Join(rec.env, " ") != "WAYLAND_DISPLAY=/run/user/1000/wayland-0" {
		t.Errorf("native env = %v, want the injected WAYLAND_DISPLAY", rec.env)
	}
}

func TestCopySSHPrefersOSC52(t *testing.T) {
	var rec recordRun
	var tty countWriter
	c := sysClipboard{argv: []string{"xclip"}, run: rec.run, preferOSC: true}
	method, err := c.copy(&tty, "hi")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if method != "osc52" {
		t.Errorf("method = %q, want osc52 in SSH", method)
	}
	if rec.called {
		t.Error("native command must not run when OSC 52 succeeds in SSH")
	}
	if tty.n != 1 {
		t.Errorf("OSC 52 should be written once, got %d", tty.n)
	}
}

func TestCopySSHFallsBackToNativeWithoutTTY(t *testing.T) {
	var rec recordRun
	c := sysClipboard{argv: []string{"xclip"}, run: rec.run, preferOSC: true}
	method, err := c.copy(nil, "hi") // no tty: OSC 52 skipped
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !rec.called || method != "xclip" {
		t.Errorf("expected native fallback (called=%v method=%q)", rec.called, method)
	}
}

func TestCopyNoNativeFallsToOSC52(t *testing.T) {
	var tty countWriter
	c := sysClipboard{argv: nil, run: func([]string, []string, string) error { return nil }, preferOSC: false}
	method, err := c.copy(&tty, "hi")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if method != "osc52" || tty.n != 1 {
		t.Errorf("expected OSC 52 fallback (method=%q writes=%d)", method, tty.n)
	}
}

func TestCopyNoMethodAvailable(t *testing.T) {
	c := sysClipboard{argv: nil, run: func([]string, []string, string) error { return nil }, preferOSC: false}
	if _, err := c.copy(nil, "hi"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable when no native cmd and no tty", err)
	}
}

func TestCopyNativeErrorFallsToOSC52(t *testing.T) {
	rec := recordRun{err: errors.New("clip failed")}
	var tty countWriter
	c := sysClipboard{argv: []string{"clip.exe"}, run: rec.run, preferOSC: false}
	method, err := c.copy(&tty, "hi")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if method != "osc52" || tty.n != 1 {
		t.Errorf("native error should fall through to OSC 52 (method=%q writes=%d)", method, tty.n)
	}
}
