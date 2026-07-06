// Package clipboard writes text to the system clipboard. It prefers a native
// OS clipboard command (clip.exe on WSL/Windows, pbcopy on macOS,
// wl-copy/xclip/xsel on Linux) for local sessions and falls back to the OSC 52
// terminal escape for remote/SSH sessions or when no native command exists.
// On a Wayland session inside tmux — where tmux strips WAYLAND_DISPLAY — the
// display is recovered by probing the runtime dir so wl-copy still works.
// Probe reports whether a native command is available (and, when not, what to
// install), backing the "install a clipboard tool" notice. The OSC 52 sequence
// builder is pure (no env, no I/O) so the bytes stay unit-testable; it has no
// TUI or git dependencies.
package clipboard

import (
	"io"
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// Mux is the terminal multiplexer the sequence must traverse, if any. Plain
// OSC 52 does not reach the outer terminal through tmux/screen without that
// multiplexer's passthrough wrapping.
type Mux int

const (
	NoMux  Mux = iota // direct terminal
	Tmux              // wrap in tmux DCS passthrough (needs `allow-passthrough on`)
	Screen            // wrap in GNU screen DCS passthrough
)

// Sequence builds the OSC 52 clipboard escape for text, wrapped for the given
// multiplexer. It is pure (no env, no I/O) so callers can assert exact bytes.
func Sequence(text string, mux Mux) string {
	s := osc52.New(text)
	switch mux {
	case Tmux:
		s = s.Tmux()
	case Screen:
		s = s.Screen()
	}
	return s.String()
}

// detectMux reads the environment to decide whether a multiplexer wrapper is
// needed. $TMUX is set inside tmux; GNU screen sets $TERM to screen* without
// setting $TMUX.
func detectMux() Mux {
	switch {
	case os.Getenv("TMUX") != "":
		return Tmux
	case strings.HasPrefix(os.Getenv("TERM"), "screen"):
		return Screen
	}
	return NoMux
}

// writeOSC52 writes the OSC 52 clipboard sequence for text to w in a SINGLE
// write. One write keeps the escape contiguous on the wire: a TUI renderer
// writing frames to the same tty from another goroutine could otherwise
// interleave a frame inside a split sequence and make the terminal fail to
// parse it. The caller is responsible for passing a tty-backed writer (see the
// TUI's isatty guard).
func writeOSC52(w io.Writer, text string) error {
	_, err := io.WriteString(w, Sequence(text, detectMux()))
	return err
}
