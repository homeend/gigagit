package clipboard

import (
	"bytes"
	"errors"
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

func TestNativeArgv(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		isWSL  bool
		env    func(string) string
		look   func(string) (string, error)
		want   []string
		wantOK bool
	}{
		{
			name: "macOS pbcopy", goos: "darwin", env: noEnv,
			look: lookOnly("pbcopy"), want: []string{"pbcopy"}, wantOK: true,
		},
		{
			name: "windows clip", goos: "windows", env: noEnv,
			look: lookOnly("clip"), want: []string{"clip"}, wantOK: true,
		},
		{
			name: "WSL prefers clip.exe over wl-copy", goos: "linux", isWSL: true,
			env:  envWith(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
			look: lookOnly("clip.exe", "wl-copy"),
			want: []string{"clip.exe"}, wantOK: true,
		},
		{
			name: "Linux Wayland wl-copy", goos: "linux", isWSL: false,
			env:  envWith(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
			look: lookOnly("wl-copy", "xclip"),
			want: []string{"wl-copy"}, wantOK: true,
		},
		{
			name: "Linux X11 xclip", goos: "linux", isWSL: false, env: noEnv,
			look: lookOnly("xclip"),
			want: []string{"xclip", "-selection", "clipboard"}, wantOK: true,
		},
		{
			name: "Linux xsel fallback", goos: "linux", isWSL: false, env: noEnv,
			look: lookOnly("xsel"),
			want: []string{"xsel", "--clipboard", "--input"}, wantOK: true,
		},
		{
			name: "Linux no tool", goos: "linux", isWSL: false, env: noEnv,
			look: lookOnly(), want: nil, wantOK: false,
		},
		{
			name: "WSL but clip.exe missing falls to xclip", goos: "linux", isWSL: true, env: noEnv,
			look: lookOnly("xclip"),
			want: []string{"xclip", "-selection", "clipboard"}, wantOK: true,
		},
		{
			name: "Wayland var set but wl-copy missing falls to xclip", goos: "linux", env: envWith(map[string]string{"WAYLAND_DISPLAY": "wayland-0"}),
			look: lookOnly("xclip"),
			want: []string{"xclip", "-selection", "clipboard"}, wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := nativeArgv(tt.goos, tt.isWSL, tt.env, tt.look)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (argv=%v)", ok, tt.wantOK, got)
			}
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("argv = %v, want %v", got, tt.want)
			}
		})
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

// recordRun captures the argv/text passed to the native command without
// spawning a process or touching the real clipboard.
type recordRun struct {
	called bool
	argv   []string
	text   string
	err    error
}

func (r *recordRun) run(argv []string, text string) error {
	r.called = true
	r.argv = argv
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
	c := sysClipboard{argv: nil, run: func([]string, string) error { return nil }, preferOSC: false}
	method, err := c.copy(&tty, "hi")
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if method != "osc52" || tty.n != 1 {
		t.Errorf("expected OSC 52 fallback (method=%q writes=%d)", method, tty.n)
	}
}

func TestCopyNoMethodAvailable(t *testing.T) {
	c := sysClipboard{argv: nil, run: func([]string, string) error { return nil }, preferOSC: false}
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
